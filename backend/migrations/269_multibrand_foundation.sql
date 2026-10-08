-- Additive foundation; existing IDs, balances, keys and orders are preserved.
CREATE TABLE IF NOT EXISTS brands (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(40) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'disabled' CHECK (status IN ('active','disabled')),
    registration_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    max_concurrent INTEGER NOT NULL DEFAULT 0 CHECK (max_concurrent >= 0),
    rpm_limit INTEGER NOT NULL DEFAULT 0 CHECK (rpm_limit >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO brands(id,code,name,status,registration_enabled) VALUES
    (1,'llmp','LLMP','active',COALESCE((SELECT value='true' FROM settings WHERE key='registration_enabled'),TRUE)),
    (2,'mues','MUES','disabled',FALSE),
    (3,'aisi','AISI','disabled',FALSE),
    (4,'opensi_codes','OpenSI Codes','disabled',FALSE),
    (5,'opensi_in','OpenSI','disabled',FALSE)
ON CONFLICT DO NOTHING;
SELECT setval(pg_get_serial_sequence('brands','id'), GREATEST((SELECT MAX(id) FROM brands),1));
CREATE TABLE IF NOT EXISTS domains (
    id BIGSERIAL PRIMARY KEY,
    brand_id BIGINT NOT NULL REFERENCES brands(id),
    hostname VARCHAR(253) NOT NULL UNIQUE CHECK (hostname = LOWER(hostname) AND hostname !~ '[/\\:@[:space:]]'),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    primary_flag BOOLEAN NOT NULL DEFAULT FALSE,
    public_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    gateway_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    overrides JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS domains_one_primary_per_brand ON domains(brand_id) WHERE primary_flag;
INSERT INTO domains(brand_id,hostname,enabled,primary_flag) VALUES
    (1,'llmp.org',TRUE,TRUE),(1,'llmp.cc',FALSE,FALSE),
    (1,'llmp.xyz',FALSE,FALSE),(1,'llmp.site',FALSE,FALSE),
    (2,'mues.cc',FALSE,TRUE),(3,'aisi.plus',FALSE,TRUE),
    (4,'opensi.codes',FALSE,TRUE),(5,'opensi.in',FALSE,TRUE)
ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS brand_settings (
    brand_id BIGINT NOT NULL REFERENCES brands(id),
    key VARCHAR(100) NOT NULL,
    value_json JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(brand_id,key)
);
CREATE TABLE IF NOT EXISTS brand_pages (
    id BIGSERIAL PRIMARY KEY,
    brand_id BIGINT NOT NULL REFERENCES brands(id),
    slug VARCHAR(100) NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    locale VARCHAR(20) NOT NULL DEFAULT 'zh',
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published')),
    title VARCHAR(200) NOT NULL,
    content_md TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1,
    published_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(brand_id,slug,locale)
);
CREATE TABLE IF NOT EXISTS brand_page_revisions (
    page_id BIGINT NOT NULL REFERENCES brand_pages(id) ON DELETE CASCADE,
    brand_id BIGINT NOT NULL REFERENCES brands(id),
    revision INTEGER NOT NULL,
    title VARCHAR(200) NOT NULL,
    content_md TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(page_id,revision)
);
CREATE TABLE IF NOT EXISTS brand_admins (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    brand_id BIGINT REFERENCES brands(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL CHECK (role IN ('super_admin','owner','operator','support')),
    CHECK ((role = 'super_admin') = (brand_id IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS brand_admins_scoped ON brand_admins(user_id,brand_id) WHERE brand_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS brand_admins_platform ON brand_admins(user_id) WHERE brand_id IS NULL;
-- An explicit, auditable grant replaces runtime inference from users.role.
INSERT INTO brand_admins(user_id,role)
SELECT id,'super_admin' FROM users WHERE role='admin' AND deleted_at IS NULL
ON CONFLICT DO NOTHING;
