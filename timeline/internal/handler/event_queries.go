package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
)

// fetchEventsAfter returns events after the given cursor, optionally filtered by category or tag.
func (s *Server) fetchEventsAfter(ctx context.Context, timelineID int64, afterDate pgtype.Date, afterSort, limit int32, categoryID, tagID int64) ([]eventCardData, error) {
	if tagID > 0 {
		rows, err := s.store.ListEventsAfterDateByTag(ctx, db.ListEventsAfterDateByTagParams{
			TimelineID:     timelineID,
			TagID:          tagID,
			AfterDate:      afterDate,
			AfterSortOrder: afterSort,
			MaxResults:     limit,
		})
		if err != nil {
			return nil, err
		}
		return mapRows(rows, func(r db.ListEventsAfterDateByTagRow) eventCardData {
			return toEventCard(r.ID, r.TimelineID, r.Slug, r.Title, r.Summary, r.EventDate, r.DatePrecision, r.SortOrder, r.CategoryID, r.CategoryName, r.CategoryColor)
		}), nil
	}

	if categoryID > 0 {
		rows, err := s.store.ListEventsAfterDateByCategory(ctx, db.ListEventsAfterDateByCategoryParams{
			TimelineID:     timelineID,
			CategoryID:     pgtype.Int8{Int64: categoryID, Valid: true},
			AfterDate:      afterDate,
			AfterSortOrder: afterSort,
			MaxResults:     limit,
		})
		if err != nil {
			return nil, err
		}
		return mapRows(rows, func(r db.ListEventsAfterDateByCategoryRow) eventCardData {
			return toEventCard(r.ID, r.TimelineID, r.Slug, r.Title, r.Summary, r.EventDate, r.DatePrecision, r.SortOrder, r.CategoryID, r.CategoryName, r.CategoryColor)
		}), nil
	}

	rows, err := s.store.ListEventsAfterDate(ctx, db.ListEventsAfterDateParams{
		TimelineID:     timelineID,
		AfterDate:      afterDate,
		AfterSortOrder: afterSort,
		MaxResults:     limit,
	})
	if err != nil {
		return nil, err
	}
	return mapRows(rows, func(r db.ListEventsAfterDateRow) eventCardData {
		return toEventCard(r.ID, r.TimelineID, r.Slug, r.Title, r.Summary, r.EventDate, r.DatePrecision, r.SortOrder, r.CategoryID, r.CategoryName, r.CategoryColor)
	}), nil
}

// fetchEventsBefore returns events before the given cursor (reversed to chronological order).
func (s *Server) fetchEventsBefore(ctx context.Context, timelineID int64, beforeDate pgtype.Date, beforeSort, limit int32, categoryID, tagID int64) ([]eventCardData, error) {
	if tagID > 0 {
		rows, err := s.store.ListEventsBeforeDateByTag(ctx, db.ListEventsBeforeDateByTagParams{
			TimelineID:      timelineID,
			TagID:           tagID,
			BeforeDate:      beforeDate,
			BeforeSortOrder: beforeSort,
			MaxResults:      limit,
		})
		if err != nil {
			return nil, err
		}
		return reverseMap(rows, func(r db.ListEventsBeforeDateByTagRow) eventCardData {
			return toEventCard(r.ID, r.TimelineID, r.Slug, r.Title, r.Summary, r.EventDate, r.DatePrecision, r.SortOrder, r.CategoryID, r.CategoryName, r.CategoryColor)
		}), nil
	}

	if categoryID > 0 {
		rows, err := s.store.ListEventsBeforeDateByCategory(ctx, db.ListEventsBeforeDateByCategoryParams{
			TimelineID:      timelineID,
			CategoryID:      pgtype.Int8{Int64: categoryID, Valid: true},
			BeforeDate:      beforeDate,
			BeforeSortOrder: beforeSort,
			MaxResults:      limit,
		})
		if err != nil {
			return nil, err
		}
		return reverseMap(rows, func(r db.ListEventsBeforeDateByCategoryRow) eventCardData {
			return toEventCard(r.ID, r.TimelineID, r.Slug, r.Title, r.Summary, r.EventDate, r.DatePrecision, r.SortOrder, r.CategoryID, r.CategoryName, r.CategoryColor)
		}), nil
	}

	rows, err := s.store.ListEventsBeforeDate(ctx, db.ListEventsBeforeDateParams{
		TimelineID:      timelineID,
		BeforeDate:      beforeDate,
		BeforeSortOrder: beforeSort,
		MaxResults:      limit,
	})
	if err != nil {
		return nil, err
	}
	return reverseMap(rows, func(r db.ListEventsBeforeDateRow) eventCardData {
		return toEventCard(r.ID, r.TimelineID, r.Slug, r.Title, r.Summary, r.EventDate, r.DatePrecision, r.SortOrder, r.CategoryID, r.CategoryName, r.CategoryColor)
	}), nil
}

func mapRows[T any](rows []T, fn func(T) eventCardData) []eventCardData {
	result := make([]eventCardData, 0, len(rows))
	for _, r := range rows {
		result = append(result, fn(r))
	}
	return result
}

func reverseMap[T any](rows []T, fn func(T) eventCardData) []eventCardData {
	result := make([]eventCardData, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		result = append(result, fn(rows[i]))
	}
	return result
}
