package handler

import (
	"encoding/json"
	"log"
	"net/http"
)

type densityBucket struct {
	Date  string `json:"date"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

func (s *Server) handleDensity(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	tl, err := s.store.GetTimeline(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.canViewTimeline(r.Context(), tl.IsPublic) {
		http.NotFound(w, r)
		return
	}

	rows, err := s.store.GetEventDensityByYear(r.Context(), tl.ID)
	if err != nil {
		log.Printf("get density: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	buckets := make([]densityBucket, 0, len(rows))
	for _, row := range rows {
		if !row.BucketDate.Valid {
			continue
		}
		buckets = append(buckets, densityBucket{
			Date:  row.BucketDate.Time.Format("2006-01-02"),
			Label: row.BucketDate.Time.Format("2006"),
			Count: row.EventCount,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	if err := json.NewEncoder(w).Encode(buckets); err != nil {
		log.Printf("encode density: %v", err)
	}
}
