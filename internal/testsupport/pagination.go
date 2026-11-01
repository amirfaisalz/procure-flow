package testsupport

// pageStart returns the zero-based index of the first item on page. Pages past
// the end clamp to total, which also keeps the multiplication from overflowing
// for extreme page numbers.
func pageStart(page, pageSize, total int) int {
	if page <= 1 || pageSize <= 0 || total <= 0 {
		return 0
	}
	if page-1 > total/pageSize {
		return total
	}
	start := (page - 1) * pageSize
	if start > total {
		return total
	}
	return start
}
