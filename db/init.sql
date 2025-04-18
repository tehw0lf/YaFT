-- run as superuser:
CREATE EXTENSION pg_cron;
CREATE EXTENSION pgcrypto;
CREATE TABLE "feature_toggles" ("id" bigserial,"key" text NOT NULL,"value" text NOT NULL,"active_at" timestamptz,"disabled_at" timestamptz,"secret" text,PRIMARY KEY ("id"),CONSTRAINT "uni_feature_toggles_key" UNIQUE ("key"));
SELECT cron.schedule('* * * * *', $$
    UPDATE feature_toggles
    SET value = CASE
        WHEN active_at <= CURRENT_DATE THEN 'true'
        ELSE value
    END;
$$);
SELECT cron.schedule('* * * * *', $$
    UPDATE feature_toggles
    SET value = CASE
        WHEN disabled_at <= CURRENT_DATE THEN 'false'
        ELSE value
    END;
$$);
