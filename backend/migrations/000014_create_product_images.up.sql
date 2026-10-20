-- Product photos. The image files live in object storage (S3-compatible); this table holds what
-- the database is good at: which product they belong to, in which order, and who may see them.
CREATE TABLE product_images (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    product_id  uuid        NOT NULL,
    -- 0 is the cover photo (shown in lists and on the storefront).
    position    integer     NOT NULL CHECK (position >= 0),
    -- Where the files are: "<object_key>.jpg" (large) and "<object_key>_thumb.jpg" (small).
    object_key  text        NOT NULL UNIQUE,
    width       integer     NOT NULL CHECK (width > 0),
    height      integer     NOT NULL CHECK (height > 0),
    created_at  timestamptz NOT NULL DEFAULT now(),

    -- Composite FK: a photo can only belong to a product of the same shop.
    CONSTRAINT product_images_product_fkey FOREIGN KEY (tenant_id, product_id)
        REFERENCES products (tenant_id, id) ON DELETE CASCADE,
    -- One photo per position. DEFERRABLE: reordering moves several rows inside one transaction,
    -- and positions may collide in between; the rule is checked at COMMIT.
    CONSTRAINT product_images_position_key UNIQUE (product_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX product_images_tenant_product_idx ON product_images (tenant_id, product_id, position);

ALTER TABLE product_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_images FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON product_images
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
