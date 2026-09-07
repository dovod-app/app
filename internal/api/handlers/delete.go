package handlers

import (
	"net/http"
)

// DeleteResearch destroys a research and everything under it. Owner only, and
// the service decides that — the route is `accessWrite` like every other write,
// because the access kind chooses the credential and the role is a separate
// question the service is the only place qualified to answer.
func (h *WriteHandler) DeleteResearch(w http.ResponseWriter, r *http.Request) {
	if err := h.research.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// DeletePreview reports what deleting this research would destroy.
//
// A read: anyone who can open the research can already see its shape, and
// gating the numbers behind ownership would leave the confirmation with nothing
// to say to an editor about why the control is not theirs.
func (h *WriteHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	summary, err := h.research.DeletionSummary(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": summary})
}

// DeleteSection removes a section. It refuses one that still holds documents
// unless `force=true` says so out loud; the refusal is a 409 carrying the count.
func (h *WriteHandler) DeleteSection(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"
	if err := h.section.Delete(r.Context(), r.PathValue("sectionId"), force); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// DeleteSession removes a session and its questions. Documents written during
// it survive, with their link to it cleared.
func (h *WriteHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if err := h.session.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// DeleteQuestion removes one question. Replies to it survive.
func (h *WriteHandler) DeleteQuestion(w http.ResponseWriter, r *http.Request) {
	if err := h.session.DeleteQuestion(r.Context(), r.PathValue("questionId")); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
