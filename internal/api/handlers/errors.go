package handlers

import (
	"errors"
	"net/http"

	"github.com/dovod-app/app/internal/service"
)

// writeServiceError turns a service error into the status it deserves.
//
// It exists because roles made the generic 500 wrong: a viewer trying to write
// is not a server fault, and a reader told "internal server error" has no idea
// they simply lack the right. Every handler that can reach a permission check
// routes its errors through here, so a new endpoint gets the mapping by
// default rather than by remembering.
//
// ErrNotFound stays 404 for both "no such thing" and "not yours" — that is the
// no-information-leak rule the access layer enforces, and the status has to
// carry it through unchanged.
func writeServiceError(w http.ResponseWriter, err error) {
	// Named refusals come first: a caller that can fix one field deserves to be
	// told which. The generic 400 below carries only a sentence.
	if errors.Is(err, service.ErrSectionInstructionLong) {
		writeFieldError(w, err.Error(), "instruction")
		return
	}
	// Checked before the sentinel list: this one carries a payload, and the only
	// useful thing a client can do with the refusal is name the fields, which a
	// bare message cannot.
	var incomplete *service.IncompleteMetadataError
	if errors.As(err, &incomplete) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":            err.Error(),
			"code":             "metadata_incomplete",
			"missing_required": incomplete.Missing,
			"hint":             "Send allow_incomplete: true to complete it anyway, or fill the fields — a value of null records an explicit unknown.",
		})
		return
	}

	switch {
	case err == nil:
		return
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrForbidden),
		errors.Is(err, service.ErrLastOwner),
		errors.Is(err, service.ErrPersonalTeam),
		errors.Is(err, service.ErrTeamNotEmpty):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrNoAuth):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, service.ErrAlreadyMember),
		errors.Is(err, service.ErrConflict),
		// 409 rather than 400: the request is well formed and the caller is
		// allowed to make it — the section's contents are what refuse, and
		// `force=true` is the retry that succeeds.
		errors.Is(err, service.ErrSectionNotEmpty):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrInviteInvalid),
		errors.Is(err, service.ErrInvalidRole),
		errors.Is(err, service.ErrTeamNameEmpty),
		errors.Is(err, service.ErrNotMember),
		errors.Is(err, service.ErrValidation),
		errors.Is(err, service.ErrMutualExclusion),
		errors.Is(err, service.ErrAnswerRequired),
		errors.Is(err, service.ErrDuplicateSectionName),
		errors.Is(err, service.ErrSectionHasNoEntries),
		errors.Is(err, service.ErrQuestionDepthLimit),
		errors.Is(err, service.ErrTextReplaceNotFound),
		errors.Is(err, service.ErrTextReplaceOnBlocks),
		errors.Is(err, service.ErrInvalidFieldSpec),
		// Import is the one write in this product that refuses rather than
		// reports, because a person is standing over the file with an undo.
		// See the comment at the top of import_markdown.go.
		errors.Is(err, service.ErrImportNotMarkdown),
		errors.Is(err, service.ErrImportTooLarge),
		errors.Is(err, service.ErrImportEmpty),
		errors.Is(err, service.ErrImportBadFrontMatter),
		errors.Is(err, service.ErrImportRejected):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
