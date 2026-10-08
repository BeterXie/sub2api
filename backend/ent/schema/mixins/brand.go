package mixins

import (
	"context"
	"fmt"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
	"github.com/Wei-Shaw/sub2api/ent/intercept"
	"github.com/Wei-Shaw/sub2api/internal/brand"
)

// BrandMixin scopes Ent queries and mutations. PostgreSQL policies provide the
// same protection to handwritten SQL and validate parent/child brand ownership.
type BrandMixin struct{ mixin.Schema }

func (BrandMixin) Fields() []ent.Field {
	return []ent.Field{field.Int64("brand_id").Default(brand.LegacyID).Immutable()}
}

func (BrandMixin) Interceptors() []ent.Interceptor {
	return []ent.Interceptor{intercept.TraverseFunc(func(ctx context.Context, q intercept.Query) error {
		if scope, ok := brand.FromContext(ctx); ok && !scope.Platform {
			q.WhereP(func(s *sql.Selector) { s.Where(sql.EQ(s.C("brand_id"), scope.ID)) })
		}
		return nil
	})}
}

func (BrandMixin) Hooks() []ent.Hook {
	return []ent.Hook{func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			scope, scoped := brand.FromContext(ctx)
			if scoped && !scope.Platform {
				if m.Op().Is(ent.OpCreate) {
					if err := m.SetField("brand_id", scope.ID); err != nil {
						return nil, err
					}
				} else {
					filter, ok := m.(interface{ WhereP(...func(*sql.Selector)) })
					if !ok {
						return nil, fmt.Errorf("%s mutation cannot be scoped", m.Type())
					}
					filter.WhereP(func(s *sql.Selector) { s.Where(sql.EQ(s.C("brand_id"), scope.ID)) })
				}
			}
			return next.Mutate(ctx, m)
		})
	}}
}
