-- run as superuser:
CREATE EXTENSION pg_cron;
CREATE EXTENSION pgcrypto;
CREATE TABLE "feature_toggles" ("id" bigserial,"key" text NOT NULL,"value" text NOT NULL,"active_at" timestamptz,"disabled_at" timestamptz,"secret" text,"tags" text[],"created_at" timestamptz,"updated_at" timestamptz,PRIMARY KEY ("id"),CONSTRAINT "uni_feature_toggles_key" UNIQUE ("key"));

-- Scheduled flips run against now(), not CURRENT_DATE.
--
-- CURRENT_DATE is midnight of the current day, so a toggle scheduled for 15:00
-- only flipped at midnight the following day, while every client library
-- evaluates the same timestamp against the wall clock and flipped at 15:00.
-- The two disagreed for up to 24 hours. now() makes the backend agree with the
-- libraries; the job runs every minute, so the flip lands within 60 seconds.
SELECT cron.schedule('* * * * *', $$
    UPDATE feature_toggles
    SET value = CASE
        WHEN active_at <= now() THEN 'true'
        ELSE value
    END;
$$);
SELECT cron.schedule('* * * * *', $$
    UPDATE feature_toggles
    SET value = CASE
        WHEN disabled_at <= now() THEN 'false'
        ELSE value
    END;
$$);

-- Retention for the public instance.
--
-- POST /features is unauthenticated for the first toggle of a group -- it has
-- to be, because that call is what issues the secret -- so anyone can create
-- rows. This job drops a whole group once none of its toggles has been touched
-- for the retention window.
--
-- Grouping is by the UUID prefix of the key: a group is deleted only when the
-- most recent updated_at across all of its toggles is older than the window,
-- so an active group never loses individual members.
--
-- This is OFF by default and is enabled per deployment, because a local stack
-- should never silently delete the operator's data. Enable it on a public
-- instance with:
--
--   SELECT cron.schedule('0 3 * * *', $$ SELECT cleanup_stale_feature_toggles(); $$);
--
-- Adjust the window by passing a different interval:
--
--   SELECT cleanup_stale_feature_toggles(INTERVAL '90 days');
CREATE OR REPLACE FUNCTION cleanup_stale_feature_toggles(retention INTERVAL DEFAULT INTERVAL '30 days')
RETURNS bigint
LANGUAGE plpgsql
AS $$
DECLARE
    deleted bigint;
BEGIN
    WITH stale AS (
        SELECT split_part(key, '|', 1) AS group_uuid
        FROM feature_toggles
        GROUP BY split_part(key, '|', 1)
        HAVING max(COALESCE(updated_at, created_at)) < now() - retention
    )
    DELETE FROM feature_toggles
    WHERE split_part(key, '|', 1) IN (SELECT group_uuid FROM stale);

    GET DIAGNOSTICS deleted = ROW_COUNT;
    RETURN deleted;
END;
$$;
