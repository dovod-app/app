package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
)

type ImportHandler struct {
	export *service.ExportService
	log    *slog.Logger
}

func NewImportHandler(export *service.ExportService, log *slog.Logger) *ImportHandler {
	return &ImportHandler{export: export, log: log}
}

func (h *ImportHandler) Import(w http.ResponseWriter, r *http.Request) {
	var data domain.ExportData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	research, warnings, err := h.export.Import(r.Context(), &data, r.URL.Query().Get("team"))
	if err != nil {
		h.log.Error("import failed", "error", err)
		writeServiceError(w, err)
		return
	}

	out := map[string]any{
		"status":      "imported",
		"research_id": research.ID,
		"code":        research.Code,
		"name":        research.Name,
	}
	// What the file carried and the import could not. Omitted when there is
	// nothing to say, so a clean import stays a clean payload.
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	writeJSON(w, http.StatusCreated, out)
}
