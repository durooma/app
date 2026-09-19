package store

import (
	"context"
	"testing"
)

func TestUserBackgroundPreferenceDefaultsOffAndIsIsolated(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	st := New(pool)
	const first, second = "settings-test-a", "settings-test-b"
	if _, err := pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id = ANY($1)`, []string{first, second}); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id = ANY($1)`, []string{first, second})
	for _, user := range []string{first, second} {
		enabled, err := st.BackgroundCategorizationEnabled(ctx, user)
		if err != nil || enabled {
			t.Fatalf("%s not off by default: %v %v", user, enabled, err)
		}
	}
	if err := st.SetBackgroundCategorization(ctx, first, true); err != nil {
		t.Fatal(err)
	}
	restarted := New(pool)
	enabled, err := restarted.BackgroundCategorizationEnabled(ctx, first)
	if err != nil || !enabled {
		t.Fatalf("preference not persisted: %v %v", enabled, err)
	}
	enabled, err = restarted.BackgroundCategorizationEnabled(ctx, second)
	if err != nil || enabled {
		t.Fatalf("preference leaked across users: %v %v", enabled, err)
	}
	if err := restarted.SetBackgroundCategorization(ctx, first, false); err != nil {
		t.Fatal(err)
	}
	enabled, err = st.BackgroundCategorizationEnabled(ctx, first)
	if err != nil || enabled {
		t.Fatalf("disable not saved: %v %v", enabled, err)
	}
}
