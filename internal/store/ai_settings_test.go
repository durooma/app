package store

import (
	"bytes"
	"context"
	"testing"
	"time"

	"durooma/internal/models"
	"durooma/internal/secrets"
)

func TestEncryptedAISettingsPersistAndRequireOwnKey(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()
	v, _ := secrets.New(bytes.Repeat([]byte{3}, 32))
	st := NewWithVault(pool, v)
	const owner = "credential-test-owner"
	const other = "credential-test-other"
	_, err := pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id=ANY($1)`, []string{owner, other})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id=ANY($1)`, []string{owner, other})
	settings, err := st.AISettings(ctx, owner)
	if err != nil || settings.HasKey || settings.Background || settings.Model != models.DefaultAIModel || settings.IntervalMS != 250 || settings.Concurrency != 8 {
		t.Fatal("bad initial settings")
	}
	settings.Background = true
	if err := st.SaveAISettings(ctx, owner, settings, "", false); err == nil {
		t.Fatal("background enabled without key")
	}
	key := "sk-or-test-private-credential"
	if err := st.SaveAISettings(ctx, owner, settings, key, false); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT openrouter_key FROM user_settings WHERE user_id=$1`, owner).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(key)) {
		t.Fatal("plaintext stored")
	}
	restarted := NewWithVault(pool, v)
	got, err := restarted.OpenRouterKey(ctx, owner)
	if err != nil || got != key {
		t.Fatal("restart lost key")
	}
	got, err = st.OpenRouterKey(ctx, other)
	if err != nil || got != "" {
		t.Fatal("credential leaked across owners")
	}
	settings.Model = "z-ai/glm-5.3-flash"
	settings.IntervalMS = 500
	settings.Concurrency = 2
	if err := restarted.SaveAISettings(ctx, owner, settings, "", false); err != nil {
		t.Fatal(err)
	}
	got, err = st.OpenRouterKey(ctx, owner)
	if err != nil || got != key {
		t.Fatal("blank update removed key")
	}
	settings, err = st.AISettings(ctx, owner)
	if err != nil || settings.Model != "z-ai/glm-5.3-flash" || settings.IntervalMS != 500 || settings.Concurrency != 2 || settings.Revision != 2 {
		t.Fatal("preferences not saved")
	}
	delay, err := st.ReserveUserAIRequest(ctx, owner, settings.Revision, time.Second)
	if err != nil || delay != 0 {
		t.Fatalf("first reservation: %v %v", delay, err)
	}
	delay, err = st.ReserveUserAIRequest(ctx, owner, settings.Revision, time.Second)
	if err != nil || delay <= 0 {
		t.Fatal("spacing not shared")
	}
	if err := st.FinishUserAIRequest(ctx, owner, settings.Revision, true, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	delay, err = restarted.ReserveUserAIRequest(ctx, owner, settings.Revision, time.Second)
	if err != nil || delay < 4*time.Second {
		t.Fatal("lost cooldown across restart")
	}
	oldRevision := settings.Revision
	if err := st.SaveAISettings(ctx, owner, settings, "sk-or-test-replacement-credential", false); err != nil {
		t.Fatal(err)
	}
	settings, _ = st.AISettings(ctx, owner)
	if err := st.FinishUserAIRequest(ctx, owner, oldRevision, true, time.Hour); err != nil {
		t.Fatal(err)
	}
	delay, err = st.ReserveUserAIRequest(ctx, owner, settings.Revision, time.Second)
	if err != nil || delay != 0 {
		t.Fatal("replacement key inherited stale budget")
	}
	if err := st.SaveAISettings(ctx, other, models.DefaultAISettings(), "sk-or-test-another-owner-key", false); err != nil {
		t.Fatal(err)
	}
	otherSettings, _ := st.AISettings(ctx, other)
	delay, err = st.ReserveUserAIRequest(ctx, other, otherSettings.Revision, time.Second)
	if err != nil || delay != 0 {
		t.Fatal("budgets leaked between owners")
	}
	if _, err := pool.Exec(ctx, `UPDATE user_settings SET openrouter_key=$2 WHERE user_id=$1`, other, stored); err != nil {
		t.Fatal(err)
	}
	if _, err := st.OpenRouterKey(ctx, other); err == nil {
		t.Fatal("copied ciphertext accepted for another owner")
	}
	if err := st.SaveAISettings(ctx, owner, settings, "", true); err != nil {
		t.Fatal(err)
	}
	after, _ := st.AISettings(ctx, owner)
	got, err = st.OpenRouterKey(ctx, owner)
	if err != nil || after.HasKey || after.Background || got != "" {
		t.Fatal("removal incomplete")
	}
	if _, err := st.ReserveUserAIRequest(ctx, owner, settings.Revision, time.Second); err == nil {
		t.Fatal("request reserved after removal")
	}
}
