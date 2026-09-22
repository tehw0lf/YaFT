package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The README examples drifted twice without anyone noticing -- they still
// advertised an `ID` field the response DTO had long since dropped, and no
// `tags`. Documentation that nothing checks is documentation that goes stale,
// so openapi.yaml is validated against the real handlers here rather than
// maintained by hand.
//
// Every case drives the actual router and validates the response body, status
// code and content type against the spec. A response the spec does not
// describe fails the test, and so does a spec that describes something the
// server does not do.

func loadSpec(t *testing.T) (*openapi3.T, func(*http.Request) (*routers.Route, map[string]string, error)) {
	t.Helper()

	loader := &openapi3.Loader{IsExternalRefsAllowed: false}
	doc, err := loader.LoadFromFile("openapi.yaml")
	require.NoError(t, err, "openapi.yaml must parse")
	require.NoError(t, doc.Validate(context.Background()), "openapi.yaml must be a valid OpenAPI document")

	router, err := gorillamux.NewRouter(doc)
	require.NoError(t, err)

	return doc, func(req *http.Request) (*routers.Route, map[string]string, error) {
		return router.FindRoute(req)
	}
}

// validateAgainstSpec checks one request/response pair against openapi.yaml.
// Both the SQLite suite here and the integration suite call it, so a response
// is validated wherever it can actually be produced.
func validateAgainstSpec(t *testing.T, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	_, find := loadSpec(t)
	validateExchange(t, find, req, rec)
}

// validateExchange checks one request/response pair against the spec.
func validateExchange(t *testing.T, find func(*http.Request) (*routers.Route, map[string]string, error), req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()

	route, pathParams, err := find(req)
	require.NoErrorf(t, err, "no operation in openapi.yaml matches %s %s", req.Method, req.URL.Path)

	err = openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: rec.Code,
		Header: rec.Header(),
		Body:   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
		},
	})
	require.NoErrorf(t, err, "%s %s -> %d: response does not match openapi.yaml\nbody: %s",
		req.Method, req.URL.Path, rec.Code, rec.Body.String())
}

// specServer is the `servers` entry from openapi.yaml. The response validator
// resolves a request against it, so test requests are addressed to it.
const specServer = "http://127.0.0.1:8080"

// TestOpenAPIMatchesHandlers drives every documented operation and validates
// what comes back against the spec.
func TestOpenAPIMatchesHandlers(t *testing.T) {
	_, find := loadSpec(t)

	testDB := setupTestDB(t)
	db = testDB
	router := setupRouter()

	// Every helper takes the active *testing.T rather than closing over the
	// outer one. require.NoErrorf calls FailNow, and doing that on the parent
	// from inside a subtest aborts the parent goroutine: the remaining
	// subtests never run, and the failure is reported against the parent
	// instead of the case that caused it.
	call := func(t *testing.T, method, path string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			require.NoError(t, json.NewEncoder(&buf).Encode(body))
		}
		// The spec's `servers` entry is part of what the validator matches on,
		// so requests must carry that host rather than httptest's default.
		req := httptest.NewRequest(method, specServer+path, &buf)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		validateExchange(t, find, req, rec)
		return rec
	}

	decode := func(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
		t.Helper()
		var out map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	// Create a group: the one response that carries a secret.
	created := decode(t, call(t, http.MethodPost, "/features", map[string]string{
		"Key":   "specKey",
		"Value": "true",
	}))
	key := created["key"].(string)
	secret := created["secret"].(string)
	uuid := strings.Split(key, keySeparator)[0]

	t.Run("create returns a secret exactly once", func(t *testing.T) {
		assert.NotEmpty(t, secret)

		second := decode(t, call(t, http.MethodPost, "/features", map[string]string{
			"Key":    uuid + keySeparator + "another",
			"Value":  "false",
			"Secret": secret,
		}))
		assert.NotContains(t, second, "secret",
			"only the call that creates a group may return the secret")
	})

	t.Run("single toggle", func(t *testing.T) {
		body := decode(t, call(t, http.MethodGet, "/features/"+key, nil))
		assert.Equal(t, key, body["key"])
	})

	t.Run("group", func(t *testing.T) {
		body := decode(t, call(t, http.MethodGet, "/features/"+uuid, nil))
		assert.Len(t, body["toggles"], 2)
	})

	t.Run("collection hash", func(t *testing.T) {
		// The hash is computed with PostgreSQL-only SQL (digest, string_agg),
		// which SQLite cannot run, so this suite only gets as far as the 404
		// branch. That still validates a documented response; the 200 body is
		// covered by TestIntegrationOpenAPICollectionHash against real
		// PostgreSQL.
		assert.Equal(t, http.StatusNotFound,
			call(t, http.MethodGet, "/collectionHash/"+uuid, nil).Code)
	})

	t.Run("activate and deactivate", func(t *testing.T) {
		assert.Equal(t, "true", decode(t, call(t, http.MethodPut,
			"/features/activate/"+key+"/"+secret, nil))["value"])
		assert.Equal(t, "false", decode(t, call(t, http.MethodPut,
			"/features/deactivate/"+key+"/"+secret, nil))["value"])
	})

	t.Run("scheduling", func(t *testing.T) {
		const when = "2026-10-10T15:00:00Z"
		assert.Equal(t, when, decode(t, call(t, http.MethodPut,
			"/features/activateAt/"+key+"/"+when+"/"+secret, nil))["activeAt"])
		assert.Equal(t, when, decode(t, call(t, http.MethodPut,
			"/features/deactivateAt/"+key+"/"+when+"/"+secret, nil))["disabledAt"])
	})

	// The error responses matter most: they are the ones a README never lists
	// in full, so a port author otherwise finds them by trial and error.
	t.Run("errors", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound,
			call(t, http.MethodGet, "/features/nosuchkey", nil).Code)

		assert.Equal(t, http.StatusUnauthorized,
			call(t, http.MethodPut, "/features/activate/"+key+"/wrongsecret", nil).Code)

		// The secret is checked before the lookup, so a bad secret hides
		// whether the key exists at all.
		assert.Equal(t, http.StatusUnauthorized,
			call(t, http.MethodPut, "/features/activate/"+uuid+keySeparator+"ghost/wrongsecret", nil).Code)

		assert.Equal(t, http.StatusBadRequest,
			call(t, http.MethodPut, "/features/activateAt/"+key+"/not-a-date/"+secret, nil).Code)

		assert.Equal(t, http.StatusBadRequest,
			call(t, http.MethodPost, "/features", map[string]string{"Key": "k", "Value": "TRUE"}).Code)

		assert.Equal(t, http.StatusBadRequest,
			call(t, http.MethodPost, "/features", map[string]string{
				"Key":   strings.Repeat("k", MaxKeyLength),
				"Value": "true",
			}).Code)
	})

	t.Run("secret rotation", func(t *testing.T) {
		newSecret := generateSecret()

		// The documented 406 is not reachable from here: the server checks the
		// new secret with url.ParseRequestURI, and httptest.NewRequest parses
		// the URL with the same rules, so any value the server would reject
		// panics while building the request. The status stays in the spec
		// because a client written against a raw socket can still trigger it.

		body := decode(t, call(t, http.MethodPut,
			"/secret/update/"+uuid+"/"+secret+"/"+newSecret, nil))
		assert.Equal(t, uuid, body["key"])

		assert.Equal(t, http.StatusUnauthorized, call(t, http.MethodPut,
			"/features/activate/"+key+"/"+secret, nil).Code,
			"the old secret must stop working")

		secret = newSecret
	})

	t.Run("delete", func(t *testing.T) {
		body := decode(t, call(t, http.MethodDelete, "/features/"+key+"/"+secret, nil))
		assert.Equal(t, "Feature toggle deleted", body["message"])

		assert.Equal(t, http.StatusNotFound,
			call(t, http.MethodGet, "/features/"+key, nil).Code)
	})
}

// TestOpenAPIVersionMatchesVERSION keeps the documented version honest. The
// spec's `info.version` is what a generated client reports, and it would
// otherwise drift on the next release without anything noticing.
func TestOpenAPIVersionMatchesVERSION(t *testing.T) {
	doc, _ := loadSpec(t)

	raw, err := os.ReadFile("VERSION")
	require.NoError(t, err)

	assert.Equal(t, strings.TrimSpace(string(raw)), doc.Info.Version,
		"info.version in openapi.yaml must match the VERSION file")
}

// TestOpenAPICoversEveryRoute fails when a route is added to the server
// without being written down, which is how a spec silently falls behind.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	doc, _ := loadSpec(t)

	documented := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			documented[method+" "+path] = true
		}
	}

	// gin reports its routes with :param, the spec uses {param}.
	for _, route := range setupRouter().Routes() {
		path := route.Path
		for _, segment := range strings.Split(path, "/") {
			if strings.HasPrefix(segment, ":") {
				path = strings.Replace(path, segment, "{"+segment[1:]+"}", 1)
			}
		}

		// The spec names the group parameter of these two `uuid`, because that
		// is what they take; gin only knows it as `key`.
		if strings.HasPrefix(path, "/collectionHash/") {
			path = "/collectionHash/{uuid}"
		}

		assert.Truef(t, documented[route.Method+" "+path],
			"%s %s is served but not in openapi.yaml", route.Method, path)
	}
}
