package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"durooma/internal/config"
	"durooma/internal/db"
	"durooma/internal/models"
	"durooma/internal/store"
)

func TestCategoryExclusionEditingAndTransactionSummaries(t *testing.T) {
	databaseURL := os.Getenv("DUROOMA_TEST_DB")
	if databaseURL == "" {
		t.Skip("set DUROOMA_TEST_DB for integration tests")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	srv, err := NewServer(&config.Config{BaseCurrency: "CHF"}, st, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	post := func(path string, values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	form := url.Values{"name": {"Category exclusion HTTP test"}, "exclude_from_calculations": {"1"}}
	if w := post("/categories", form); w.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	id, found, err := st.CategoryByName(ctx, form.Get("name"))
	if err != nil || !found {
		t.Fatalf("created category missing: %v", err)
	}
	defer st.DeleteCategory(ctx, id)
	account, err := st.CreateAccount(ctx, "Category HTTP test", "Main", "CHF")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM institutions WHERE name='Category HTTP test'`)
	month := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, amount := range []float64{1200, -30} {
		var category *int64
		if i == 0 {
			category = &id
		}
		if _, err := st.InsertTransactions(ctx, []models.Transaction{{AccountID: account,
			Date: month, Description: fmt.Sprintf("Exclusion HTTP test %d", i), Amount: amount, BaseAmount: amount,
			Currency: "CHF", BaseCurrency: "CHF", StartMonth: month, EndMonth: month.AddDate(0, 2, 0),
			CategoryID: category, ExternalHash: fmt.Sprintf("http-exclusion-%d", i),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	path := fmt.Sprintf("/transactions?account_id=%d&period=2024-01", account)
	for _, excluded := range []bool{true, false, true} {
		if excluded {
			form.Set("exclude_from_calculations", "1")
		} else {
			form.Del("exclude_from_calculations")
		}
		if w := post(fmt.Sprintf("/categories/%d", id), form); w.Code != http.StatusSeeOther {
			t.Fatalf("save: %d %s", w.Code, w.Body)
		}
		category, err := st.GetCategory(ctx, id)
		if err != nil || category.ExcludeFromCalculations != excluded {
			t.Fatalf("saved exclusion: %+v %v", category, err)
		}
		for _, includedOnly := range []bool{false, true} {
			requestPath := path
			if includedOnly {
				requestPath += "&included_only=1"
			}
			r := httptest.NewRequest("GET", requestPath, nil)
			data, err := srv.transactionsData(r, txnFilterFromRequest(r))
			if err != nil {
				t.Fatal(err)
			}
			want, count := 1170.0, 2
			if excluded {
				want = -30
				if includedOnly {
					count = 1
				}
			}
			if data["Total"] != want || data["AllocTotal"] != want/3 || len(data["Transactions"].([]models.Transaction)) != count {
				t.Fatalf("excluded=%v includedOnly=%v: total=%v allocated=%v", excluded, includedOnly, data["Total"], data["AllocTotal"])
			}
			next, err := url.Parse(data["NextPageURL"].(string))
			if err != nil || next.Query().Get("period") != "2024-01" || next.Query().Get("account_id") != fmt.Sprint(account) || (next.Query().Get("included_only") == "1") != includedOnly {
				t.Fatal("pagination lost filters")
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/categories", nil))
		checkbox := regexp.MustCompile(fmt.Sprintf(`<input form="cat-%d" type="checkbox"[^>]*>`, id)).FindString(w.Body.String())
		if checkbox == "" || strings.Contains(checkbox, "checked") != excluded {
			t.Fatalf("checkbox did not reflect persisted setting: %s", checkbox)
		}
	}
	rows, err := st.ListTransactions(ctx, store.TxnFilter{AccountID: account, CategoryID: id})
	if err != nil || len(rows) != 1 {
		t.Fatalf("transfer transaction: %v", err)
	}
	if w := post(fmt.Sprintf("/transactions/%d/category", rows[0].ID), url.Values{"category_id": {"0"}}); w.Code != http.StatusNoContent || w.Header().Get("HX-Refresh") != "true" {
		t.Fatal("recategorization must refresh displayed totals")
	}
}

func TestAllReportDrillLinksRespectCategoryExclusions(t *testing.T) {
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, period := range []models.Period{{}, {Year: 2024}, {Year: 2024, Month: 6}} {
		var out strings.Builder
		if err := tmpls.pages["reports"].ExecuteTemplate(&out, "layout.html", reportData(period, nil)); err != nil {
			t.Fatal(err)
		}
		links := regexp.MustCompile(`href="(/transactions\?[^"]+)"`).FindAllStringSubmatch(out.String(), -1)
		if len(links) == 0 {
			t.Fatal("no report drill links found")
		}
		for _, link := range links {
			if !strings.Contains(link[1], "included_only=1") {
				t.Fatalf("report link includes excluded transactions: %s", link[1])
			}
		}
	}
}
