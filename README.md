# YaFT - Yet another Feature Toggle

<div align="center">
  <img src="./logo.svg" alt="YaFT Logo" width="140">
</div>

---

Provides a simple feature toggle API that supports strings as keys and stringified booleans (`"true"` or `"false"`) as values.
Features can be given an optional start and end date.
Features are grouped by prepending UUIDs. This is done automatically on creating the first feature in a group. To add more features to the group, prepend the new feature's key with a UUIDv4 followed by a pipe symbol `|` (see examples)
All endpoints apart from the `GET` endpoint require a secret. This is created when creating a new feature toggle without a UUIDv4 and returned from the `POST` request. The secret is only returned upon creating the _first_ feature toggle in a group.

---

# Usage:
## Prod usage (EARLY ALPHA)
Please note that this program is in an alpha state and has not been tested. Use at your own risk!
Copy and adapt `docker-compose.yml` or build your own setup.

## Local usage (for development)
`docker compose -f docker-compose-local.yml up --force-recreate --build`

# API Interaction

## Creating new Feature Toggles

`curl -d '{"Key":"myKey","Value":"true"}' -X POST "http://127.0.0.1:8080/features"`

### Responses

successful response:
`{"activeAt":null,"disabledAt":null,"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","secret":"156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9","value":"true"}`

error response:
`{"error":"Failed to create feature toggle"}`

## Creating new Feature Toggles with existing UUID

`curl -d '{"Key":"896ea308-382f-46b0-bc59-d93a28013633|myOtherKey","Value":"true","Secret":"156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9"}' -X POST "http://127.0.0.1:8080/features"`

### Responses

successful response:
`{"activeAt":null,"disabledAt":null,"key":"896ea308-382f-46b0-bc59-d93a28013633|myOtherKey","value":"true"}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but key exists:
`{"error":"Failed to create feature toggle"}`

## Deleting a specific Feature Toggle

`curl -X DELETE "http://127.0.0.1:8080/features/896ea308-382f-46b0-bc59-d93a28013633|myKey/156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9"`

### Responses

successful response:
`{"message":"Feature toggle deleted"}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Feature not found"}`

## Activate a Feature Toggle

`curl -X PUT "http://127.0.0.1:8080/features/activate/896ea308-382f-46b0-bc59-d93a28013633|myKey/156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9"`

### Responses

successful response:
`{"activeAt":null,"disabledAt":null,"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true"}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to activate feature toggle"}`

## Activate a Feature Toggle at a certain date

`curl -X PUT "http://127.0.0.1:8080/features/activateAt/896ea308-382f-46b0-bc59-d93a28013633|myKey/2026-10-10/156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9"`

### Responses

successful response:
`{"activeAt":2026-10-10,"disabledAt":null,"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true"}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to activate feature toggle at"}`

## Deactivate a Feature Toggle

`curl -X PUT "http://127.0.0.1:8080/features/deactivate/896ea308-382f-46b0-bc59-d93a28013633|myKey/156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9"`

### Responses

successful response:
`{"activeAt":null,"disabledAt":null,"key":"88ce4805-92a5-4774-ac05-5ebf12de9a58|a","value":"false"}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to deactivate feature toggle"}`

## Deactivate a Feature Toggle at a certain date

`curl -X PUT "http://127.0.0.1:8080/features/deactivateAt/896ea308-382f-46b0-bc59-d93a28013633|myKey/2026-10-10/156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9"`

### Responses

successful response:
`{"activeAt":null,"disabledAt":2026-10-10,"key":"88ce4805-92a5-4774-ac05-5ebf12de9a58|a","value":"false"}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to deactivate feature toggle"}`

## Getting a specific Feature Toggle

`curl "http://127.0.0.1:8080/features/896ea308-382f-46b0-bc59-d93a28013633|myKey"`

### Responses

successful response:
`{"activeAt":null,"disabledAt":null,"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true"}`

error response:
`{"error":"Feature not found"}`

## Getting all Feature Toggles for a given UUID

`curl "http://127.0.0.1:8080/features/896ea308-382f-46b0-bc59-d93a28013633"`

### Responses

successful response:
`{"toggles":[{"ID":20,"Key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","Value":"true","ActiveAt":null,"DisabledAt":null},{"ID":21,"Key":"896ea308-382f-46b0-bc59-d93a28013633|myOtherKey","Value":"true","ActiveAt":null,"DisabledAt":null}]}`

## Getting the collection hash for a given UUID

`curl "http://127.0.0.1:8080/collectionHash/896ea308-382f-46b0-bc59-d93a28013633"`

### Responses

successful response:
`{"collectionHash":"dce01876b3f0c843fb2c1e5efe54bf807dc991eefc660d112306b49f6e2335c6"}`

error response:
`{"error":"Feature not found"}`

## Updating a secret for a given UUID

`curl -X PUT "http://127.0.0.1:8080/secret/update/896ea308-382f-46b0-bc59-d93a28013633/156152c0-07c6-4c87-b73a-b10db750bca3aa88c846-ce3f-48af-8fc0-e42a7b92f7321c8af6bc-b8a8-4bd8-88a5-53215bb82ae9/mynewsecret"`

### Responses

successful response:
`{"key":"896ea308-382f-46b0-bc59-d93a28013633"}`

error response if secret is invalid or UUID does not exist:
`{"error":"Invalid secret"}`

error response if new secret is not URL parseable:
`{"error": "New secret is not URL parseable, aborting operation"}`

# Licenses

- Code: MIT License
- Logo/Branding: All rights reserved