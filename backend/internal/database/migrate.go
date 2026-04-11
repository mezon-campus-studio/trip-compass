package database

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			// Baseline: database was initialized by database/schema.sql.
			ID: "202604020001_baseline",
			Migrate: func(tx *gorm.DB) error {
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Forward migration example: keep idempotent for existing environments.
			ID: "202604020002_activity_place_constraints",
			Migrate: func(tx *gorm.DB) error {
				sqls := []string{
					`ALTER TABLE activities ADD COLUMN IF NOT EXISTS place_id UUID;`,
					`DO $$
BEGIN
	IF to_regclass('places') IS NOT NULL
	   AND to_regclass('activities') IS NOT NULL
	   AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_activities_place') THEN
        ALTER TABLE activities
        ADD CONSTRAINT fk_activities_place
        FOREIGN KEY (place_id) REFERENCES places(id) ON DELETE SET NULL;
    END IF;
END $$;`,
					`DO $$
BEGIN
	IF to_regclass('activities') IS NOT NULL
	   AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uq_activity_order') THEN
        ALTER TABLE activities
        ADD CONSTRAINT uq_activity_order UNIQUE (itinerary_id, day_number, order_index);
    END IF;
END $$;`,
				}

				for _, q := range sqls {
					if err := tx.Exec(q).Error; err != nil {
						return err
					}
				}

				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Add images column to places table.
			ID: "202604020003_add_images_to_places",
			Migrate: func(tx *gorm.DB) error {
				return tx.Exec(`ALTER TABLE places ADD COLUMN IF NOT EXISTS images text[];`).Error
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Add planner fields to places table.
			ID: "202604040004_add_planner_fields_to_places",
			Migrate: func(tx *gorm.DB) error {
				sqls := []string{
					`ALTER TABLE places ADD COLUMN IF NOT EXISTS must_visit boolean NOT NULL DEFAULT false;`,
					`ALTER TABLE places ADD COLUMN IF NOT EXISTS priority_score integer NOT NULL DEFAULT 0;`,
					`ALTER TABLE places ADD COLUMN IF NOT EXISTS best_time_of_day varchar(20) DEFAULT 'any';`,
					`ALTER TABLE places ADD COLUMN IF NOT EXISTS tags text[] DEFAULT '{}';`,
					`ALTER TABLE places ADD COLUMN IF NOT EXISTS open_time time without time zone;`,
					`ALTER TABLE places ADD COLUMN IF NOT EXISTS close_time time without time zone;`,
					`CREATE INDEX IF NOT EXISTS idx_place_must_visit ON places (must_visit) WHERE must_visit = true;`,
					`CREATE INDEX IF NOT EXISTS idx_place_priority ON places (destination, priority_score DESC);`,
				}
				for _, q := range sqls {
					if err := tx.Exec(q).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
	})

	return m.Migrate()
}
