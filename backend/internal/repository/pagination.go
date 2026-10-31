package repository

import "github.com/Wei-Shaw/sub2api/internal/pkg/pagination"

func paginationResultFromTotal(total int64, params pagination.PaginationParams) *pagination.PaginationResult {
	pages := int(total) / params.Limit()
	if int(total)%params.Limit() > 0 {
		pages++
	}
	return &pagination.PaginationResult{
		Total:    total,
		Page:     params.Page,
		PageSize: params.Limit(),
		Pages:    pages,
	}
}

func paginateSlice[T any](items []T, params pagination.PaginationParams) []T {
	if len(items) == 0 {
		return []T{}
	}

	offset := params.Offset()
	limit := params.Limit()
	if offset < 0 || limit <= 0 || offset >= len(items) {
		return []T{}
	}
	end := len(items)
	if limit <= len(items)-offset {
		end = offset + limit
	}
	return items[offset:end]
}
