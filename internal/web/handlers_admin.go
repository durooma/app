package web

import (
	"net/http"

	"durooma/internal/models"
)

// --- Categories ---

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cats, err := s.store.ListCategories(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	data := s.base(ctx, "Categories", "categories")
	data["Categories"] = cats
	s.templates.render(w, "categories", data)
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	c := models.Category{
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Kind:        r.FormValue("kind"),
		SortOrder:   intParam(r, "sort_order", 0),
	}
	if c.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if _, err := s.store.CreateCategory(r.Context(), c); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/categories", http.StatusSeeOther)
}

func (s *Server) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	c := models.Category{
		ID:          id,
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Kind:        r.FormValue("kind"),
		SortOrder:   intParam(r, "sort_order", 0),
	}
	if err := s.store.UpdateCategory(r.Context(), c); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/categories", http.StatusSeeOther)
}

func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteCategory(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/categories", http.StatusSeeOther)
}

// --- Rules ---

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rules, err := s.store.ListRules(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	cats, _ := s.store.ListCategories(ctx)
	data := s.base(ctx, "Rules", "rules")
	data["Rules"] = rules
	data["Categories"] = cats
	s.templates.render(w, "rules", data)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	pattern := r.FormValue("pattern")
	catID := int64(intParam(r, "category_id", 0))
	if pattern == "" || catID == 0 {
		http.Error(w, "pattern and category are required", http.StatusBadRequest)
		return
	}
	if err := s.store.CreateRule(r.Context(), pattern, catID, intParam(r, "priority", 0)); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/rules", http.StatusSeeOther)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := int64PathValue(r, "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteRule(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/rules", http.StatusSeeOther)
}

// --- Accounts ---

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	data := s.base(ctx, "Accounts", "accounts")
	data["Accounts"] = accounts
	s.templates.render(w, "accounts", data)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	inst := r.FormValue("institution")
	name := r.FormValue("name")
	currency := r.FormValue("currency")
	if inst == "" || name == "" || currency == "" {
		http.Error(w, "institution, name and currency are required", http.StatusBadRequest)
		return
	}
	if _, err := s.store.CreateAccount(r.Context(), inst, name, currency); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}
