package api

import (
	"log"
	"net/http"
	"strconv"
)

// GET /buses/{sptrans_id}
func (h *Handler) BusPositions(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("sptrans_id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		jsonError(w, "sptrans_id inválido", http.StatusBadRequest)
		return
	}

	positions, err := h.sptrans.GetBusPositionsByLine(id)
	if err != nil {
		log.Printf("[BusPositions] erro: %v", err)
		jsonError(w, "falha ao buscar posições", http.StatusBadGateway)
		return
	}

	jsonOK(w, map[string]any{"buses": positions})
}
