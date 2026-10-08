package brand

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Store owns brand lifecycle, presentation and operator grants. Billing and
// upstream scheduling remain in their existing modules.
type Store struct{ DB *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{DB: db} }

type Brand struct {
	ID                  int64  `json:"id"`
	Code                string `json:"code"`
	Name                string `json:"name"`
	Status              string `json:"status"`
	RegistrationEnabled bool   `json:"registration_enabled"`
	MaxConcurrent       int    `json:"max_concurrent"`
	RPMLimit            int    `json:"rpm_limit"`
}
type Domain struct {
	ID             int64          `json:"id"`
	BrandID        int64          `json:"brand_id"`
	Hostname       string         `json:"hostname"`
	Enabled        bool           `json:"enabled"`
	Primary        bool           `json:"primary_flag"`
	PublicEnabled  bool           `json:"public_enabled"`
	GatewayEnabled bool           `json:"gateway_enabled"`
	Overrides      map[string]any `json:"overrides"`
	Metadata       map[string]any `json:"metadata"`
}
type Grant struct {
	UserID  int64  `json:"user_id"`
	BrandID *int64 `json:"brand_id"`
	Role    string `json:"role"`
}
type Page struct {
	ID          int64      `json:"id"`
	BrandID     int64      `json:"brand_id"`
	Slug        string     `json:"slug"`
	Locale      string     `json:"locale"`
	Status      string     `json:"status"`
	Title       string     `json:"title"`
	Category    string     `json:"category"`
	SortOrder   int        `json:"sort_order"`
	ContentMD   string     `json:"content_md"`
	Revision    int        `json:"revision"`
	PublishedAt *time.Time `json:"published_at"`
}

func (s *Store) Resolve(ctx context.Context, host string) (Scope, error) {
	host, err := NormalizeHost(host)
	if err != nil {
		return Scope{}, err
	}
	var scope Scope
	var enabled bool
	var status, canonicalAPIHost string
	err = s.DB.QueryRowContext(ctx, `SELECT b.id,b.code,b.name,b.status,b.registration_enabled,b.max_concurrent,b.rpm_limit,
		d.id,d.hostname,d.enabled,d.public_enabled,d.gateway_enabled,
		COALESCE(api.hostname,'') FROM domains d JOIN brands b ON b.id=d.brand_id
		LEFT JOIN LATERAL (
			SELECT candidate.hostname FROM domains candidate
			WHERE candidate.brand_id=b.id AND candidate.enabled AND candidate.gateway_enabled
			ORDER BY candidate.primary_flag DESC,candidate.id
			LIMIT 1
		) api ON TRUE WHERE d.hostname=$1`, host).
		Scan(&scope.ID, &scope.Code, &scope.Name, &status, &scope.RegistrationEnabled, &scope.MaxConcurrent, &scope.RPMLimit,
			&scope.DomainID, &scope.Hostname, &enabled, &scope.PublicEnabled, &scope.GatewayEnabled, &canonicalAPIHost)
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, ErrUnknownDomain
	}
	if err != nil {
		return Scope{}, err
	}
	if !enabled || status != "active" {
		return Scope{}, ErrUnavailable
	}
	if canonicalAPIHost != "" {
		scope.CanonicalAPIOrigin = "https://" + canonicalAPIHost
	}
	return scope, nil
}

func (s *Store) GetBrand(ctx context.Context, id int64) (Brand, error) {
	var b Brand
	err := s.DB.QueryRowContext(ctx, "SELECT id,code,name,status,registration_enabled,max_concurrent,rpm_limit FROM brands WHERE id=$1", id).
		Scan(&b.ID, &b.Code, &b.Name, &b.Status, &b.RegistrationEnabled, &b.MaxConcurrent, &b.RPMLimit)
	return b, err
}

// ScopeForBrand is for authenticated operations on an already-owned record.
// Pending payments can settle even after a site's public traffic is disabled.
func (s *Store) ScopeForBrand(ctx context.Context, id int64) (Scope, error) {
	b, err := s.GetBrand(ctx, id)
	if err != nil {
		return Scope{}, err
	}
	scope := Scope{ID: b.ID, Code: b.Code, Name: b.Name, RegistrationEnabled: b.RegistrationEnabled, MaxConcurrent: b.MaxConcurrent, RPMLimit: b.RPMLimit}
	domains, err := s.Domains(ctx, id)
	if err != nil {
		return Scope{}, err
	}
	selected := -1
	for i := range domains {
		if domains[i].Enabled && domains[i].Primary {
			selected = i
			break
		}
	}
	if selected < 0 {
		for i := range domains {
			if domains[i].Enabled {
				selected = i
				break
			}
		}
	}
	if selected < 0 {
		for i := range domains {
			if domains[i].Primary {
				selected = i
				break
			}
		}
	}
	if selected < 0 && len(domains) > 0 {
		selected = 0
	}
	if selected >= 0 {
		domain := domains[selected]
		scope.DomainID = domain.ID
		scope.Hostname = domain.Hostname
		scope.PublicEnabled = domain.PublicEnabled
		scope.GatewayEnabled = domain.GatewayEnabled
	}
	for _, domain := range domains {
		if domain.Enabled && domain.GatewayEnabled {
			scope.CanonicalAPIOrigin = "https://" + domain.Hostname
			break
		}
	}
	return scope, nil
}
func (s *Store) List(ctx context.Context) ([]Brand, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,code,name,status,registration_enabled,max_concurrent,rpm_limit FROM brands ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Brand{}
	for rows.Next() {
		var b Brand
		if err := rows.Scan(&b.ID, &b.Code, &b.Name, &b.Status, &b.RegistrationEnabled, &b.MaxConcurrent, &b.RPMLimit); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) SaveBrand(ctx context.Context, b *Brand) error {
	if !ValidSlug(b.Code) || b.Name == "" || (b.Status != "active" && b.Status != "disabled") || b.MaxConcurrent < 0 || b.RPMLimit < 0 {
		return fmt.Errorf("invalid brand")
	}
	if b.ID == 0 {
		return s.DB.QueryRowContext(ctx, "INSERT INTO brands(code,name,status,registration_enabled,max_concurrent,rpm_limit) VALUES($1,$2,$3,$4,$5,$6) RETURNING id", b.Code, b.Name, b.Status, b.RegistrationEnabled, b.MaxConcurrent, b.RPMLimit).Scan(&b.ID)
	}
	// Codes determine asset paths and remain immutable.
	result, err := s.DB.ExecContext(ctx, "UPDATE brands SET name=$2,status=$3,registration_enabled=$4,max_concurrent=$5,rpm_limit=$6,updated_at=NOW() WHERE id=$1", b.ID, b.Name, b.Status, b.RegistrationEnabled, b.MaxConcurrent, b.RPMLimit)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return err
}
func (s *Store) DeleteBrand(ctx context.Context, id int64) error {
	if id == LegacyID {
		return fmt.Errorf("LLMP cannot be deleted")
	}
	// Soft deactivation preserves users, orders and financial audit history.
	_, err := s.DB.ExecContext(ctx, "UPDATE brands SET status='disabled',registration_enabled=FALSE,updated_at=NOW() WHERE id=$1", id)
	return err
}
func (s *Store) Domains(ctx context.Context, id int64) ([]Domain, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,brand_id,hostname,enabled,primary_flag,public_enabled,gateway_enabled,overrides,metadata FROM domains WHERE brand_id=$1 ORDER BY primary_flag DESC,id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		var raw, meta []byte
		if err := rows.Scan(&d.ID, &d.BrandID, &d.Hostname, &d.Enabled, &d.Primary, &d.PublicEnabled, &d.GatewayEnabled, &raw, &meta); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &d.Overrides); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(meta, &d.Metadata); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) SaveDomain(ctx context.Context, d *Domain) error {
	host, err := NormalizeHost(d.Hostname)
	if err != nil || strings.Contains(d.Hostname, ":") || d.BrandID <= 0 {
		return fmt.Errorf("invalid domain")
	}
	d.Hostname = host
	if err := ValidateSettings(d.Overrides, true); err != nil {
		return err
	}
	raw, err := json.Marshal(d.Overrides)
	if err != nil {
		return err
	}
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(d.Metadata)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize primary-domain changes per brand.
	if _, err := tx.ExecContext(ctx, "SELECT id FROM brands WHERE id=$1 FOR UPDATE", d.BrandID); err != nil {
		return err
	}
	if d.Primary {
		if _, err := tx.ExecContext(ctx, "UPDATE domains SET primary_flag=FALSE WHERE brand_id=$1", d.BrandID); err != nil {
			return err
		}
	}
	if d.ID == 0 {
		err = tx.QueryRowContext(ctx, "INSERT INTO domains(brand_id,hostname,enabled,primary_flag,public_enabled,gateway_enabled,overrides,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id", d.BrandID, d.Hostname, d.Enabled, d.Primary, d.PublicEnabled, d.GatewayEnabled, string(raw), string(meta)).Scan(&d.ID)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE domains SET hostname=$3,enabled=$4,primary_flag=$5,public_enabled=$6,gateway_enabled=$7,overrides=$8,metadata=$9,updated_at=NOW() WHERE id=$1 AND brand_id=$2", d.ID, d.BrandID, d.Hostname, d.Enabled, d.Primary, d.PublicEnabled, d.GatewayEnabled, string(raw), string(meta))
		if err == nil {
			var n int64
			n, err = result.RowsAffected()
			if err == nil && n == 0 {
				err = sql.ErrNoRows
			}
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteDomain(ctx context.Context, id, domainID int64) error {
	result, err := s.DB.ExecContext(ctx, "DELETE FROM domains WHERE id=$1 AND brand_id=$2 AND NOT primary_flag", domainID, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("domain missing or primary")
	}
	return err
}
func (s *Store) AdminRole(ctx context.Context, userID, brandID int64) (string, error) {
	var role string
	err := s.DB.QueryRowContext(ctx, `SELECT role FROM brand_admins WHERE user_id=$1 AND (brand_id=$2 OR brand_id IS NULL)
		ORDER BY (brand_id IS NULL) DESC LIMIT 1`, userID, brandID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}
func (s *Store) Grants(ctx context.Context, id int64) ([]Grant, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT user_id,brand_id,role FROM brand_admins WHERE brand_id=$1 ORDER BY user_id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.UserID, &g.BrandID, &g.Role); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Store) SaveGrant(ctx context.Context, g Grant) error {
	if g.BrandID == nil || g.UserID <= 0 || (g.Role != "owner" && g.Role != "operator" && g.Role != "support") {
		return fmt.Errorf("invalid grant")
	}
	// Operators must be actual users of the assigned brand.
	var id int64
	if err := s.DB.QueryRowContext(ctx, "SELECT id FROM users WHERE id=$1 AND brand_id=$2 AND deleted_at IS NULL", g.UserID, *g.BrandID).Scan(&id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO brand_admins(user_id,brand_id,role) VALUES($1,$2,$3)
		ON CONFLICT(user_id,brand_id) WHERE brand_id IS NOT NULL DO UPDATE SET role=EXCLUDED.role`, g.UserID, *g.BrandID, g.Role)
	return err
}
func (s *Store) DeleteGrant(ctx context.Context, id, userID int64) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM brand_admins WHERE brand_id=$1 AND user_id=$2", id, userID)
	return err
}
func (s *Store) Settings(ctx context.Context, scope Scope) (map[string]any, error) {
	if scope.Code == "" || scope.Hostname == "" {
		full, err := s.ScopeForBrand(ctx, scope.ID)
		if err != nil {
			return nil, err
		}
		scope = full
	}
	out := Defaults(scope)
	if scope.ID == LegacyID {
		out = map[string]any{"home_template": "llmp"}
		legacy, err := s.DB.QueryContext(ctx, "SELECT key,value FROM settings")
		if err != nil {
			return nil, err
		}
		for legacy.Next() {
			var key, value string
			if err := legacy.Scan(&key, &value); err != nil {
				legacy.Close()
				return nil, err
			}
			if AllowedSetting(key) {
				out[key] = value
			}
		}
		err = legacy.Err()
		legacy.Close()
		if err != nil {
			return nil, err
		}
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT key,value_json FROM brand_settings WHERE brand_id=$1", scope.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if !AllowedSetting(key) {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			rows.Close()
			return nil, err
		}
		out[key] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if scope.DomainID > 0 {
		var raw []byte
		if err := s.DB.QueryRowContext(ctx, "SELECT overrides FROM domains WHERE id=$1 AND brand_id=$2", scope.DomainID, scope.ID).Scan(&raw); err != nil {
			return nil, err
		}
		var override map[string]any
		if err := json.Unmarshal(raw, &override); err != nil {
			return nil, err
		}
		for k, v := range override {
			if DomainSetting(k) {
				out[k] = v
			}
		}
	}
	out["registration_enabled"] = scope.RegistrationEnabled
	out["api_base_url"] = scope.CanonicalAPIOrigin
	out["frontend_url"] = "https://" + scope.Hostname
	return out, nil
}
func (s *Store) SaveSettings(ctx context.Context, id int64, values map[string]any) error {
	if err := ValidateSettings(values, false); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if SecretSetting(key) && value == "" {
			continue
		}
		if key == "default_subscriptions" || strings.HasSuffix(key, "_subscriptions") {
			var entries []struct {
				GroupID int64 `json:"group_id"`
			}
			raw, _ := json.Marshal(value)
			if text, ok := value.(string); ok {
				raw = []byte(text)
			}
			if json.Unmarshal(raw, &entries) != nil {
				return fmt.Errorf("invalid subscriptions")
			}
			for _, entry := range entries {
				var allowed bool
				if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND brand_id=$2 AND deleted_at IS NULL)", entry.GroupID, id).Scan(&allowed); err != nil {
					return err
				}
				if !allowed {
					return ErrScope
				}
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO brand_settings(brand_id,key,value_json) VALUES($1,$2,$3)
		ON CONFLICT(brand_id,key) DO UPDATE SET value_json=EXCLUDED.value_json,updated_at=NOW()`, id, key, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Pages(ctx context.Context, id int64, published bool) ([]Page, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,brand_id,slug,locale,status,title,content_md,revision,published_at,category,sort_order
		FROM brand_pages WHERE brand_id=$1 AND (NOT $2 OR status='published') ORDER BY category,sort_order,slug,locale`, id, published)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Page{}
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.BrandID, &p.Slug, &p.Locale, &p.Status, &p.Title, &p.ContentMD, &p.Revision, &p.PublishedAt, &p.Category, &p.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) Page(ctx context.Context, id int64, slug, locale string, published bool) (Page, error) {
	var p Page
	err := s.DB.QueryRowContext(ctx, `SELECT id,brand_id,slug,locale,status,title,content_md,revision,published_at,category,sort_order
		FROM brand_pages WHERE brand_id=$1 AND slug=$2 AND locale=$3 AND (NOT $4 OR status='published')`, id, slug, locale, published).
		Scan(&p.ID, &p.BrandID, &p.Slug, &p.Locale, &p.Status, &p.Title, &p.ContentMD, &p.Revision, &p.PublishedAt, &p.Category, &p.SortOrder)
	return p, err
}
func (s *Store) SavePage(ctx context.Context, p *Page) error {
	if !ValidSlug(p.Slug) || p.Title == "" || len(p.Title) > 200 || len(p.ContentMD) > 2<<20 || (p.Status != "draft" && p.Status != "published") {
		return fmt.Errorf("invalid page")
	}
	if p.Locale == "" {
		p.Locale = "zh"
	}
	if len(p.Locale) > 20 {
		return fmt.Errorf("invalid locale")
	}
	if len(p.Category) > 200 || strings.ContainsAny(p.Category, "\r\n") {
		return fmt.Errorf("invalid category")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Revision is an optimistic concurrency token supplied by the editor.
	if p.ID == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO brand_pages(brand_id,slug,locale,status,title,content_md,published_at,category,sort_order)
			VALUES($1,$2,$3,$4::varchar,$5,$6,CASE WHEN $4::varchar='published' THEN NOW() END,$7,$8) RETURNING id,revision`, p.BrandID, p.Slug, p.Locale, p.Status, p.Title, p.ContentMD, p.Category, p.SortOrder).Scan(&p.ID, &p.Revision)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE brand_pages SET slug=$3,locale=$4,status=$5::varchar,title=$6,content_md=$7,
			revision=revision+1,published_at=CASE WHEN $5::varchar='published' THEN NOW() END,updated_at=NOW(),
			category=$9,sort_order=$10
			WHERE id=$1 AND brand_id=$2 AND revision=$8 RETURNING revision`, p.ID, p.BrandID, p.Slug, p.Locale, p.Status, p.Title, p.ContentMD, p.Revision, p.Category, p.SortOrder).Scan(&p.Revision)
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO brand_page_revisions(page_id,brand_id,revision,title,content_md,status,category,sort_order)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.ID, p.BrandID, p.Revision, p.Title, p.ContentMD, p.Status, p.Category, p.SortOrder)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Revisions(ctx context.Context, id, pageID int64) ([]Page, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT page_id,brand_id,title,content_md,status,revision,category,sort_order FROM brand_page_revisions WHERE brand_id=$1 AND page_id=$2 ORDER BY revision DESC", id, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Page{}
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.BrandID, &p.Title, &p.ContentMD, &p.Status, &p.Revision, &p.Category, &p.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func ValidSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > 100 {
		return false
	}
	for i, r := range slug {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && (i == 0 || (r != '-' && r != '_')) {
			return false
		}
	}
	return true
}
