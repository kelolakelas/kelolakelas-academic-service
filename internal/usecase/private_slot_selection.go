package usecase

import "github.com/kelolakelas/kelolakelas-academic-service/internal/domain"

func samePrivateSlots(left, right []domain.PrivateScheduleSlot) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func samePrivateLimit(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func privateSelectedSlots(request *domain.PrivateScheduleRequest, approval *domain.ApprovePrivateScheduleRequest) ([]domain.PrivateScheduleSlot, error) {
	if approval == nil {
		if len(request.ApprovedSlots) > 0 {
			return request.ApprovedSlots, nil
		}
		return request.Slots, nil
	}
	if !samePrivateSlots(approval.ProposalSnapshot, request.Slots) {
		return nil, domain.ErrPrivateSelectionStale
	}
	if len(approval.SelectedSlotIndexes) == 0 {
		return nil, domain.ErrPrivateSelection
	}
	indexes := make(map[int]bool, len(approval.SelectedSlotIndexes))
	for _, index := range approval.SelectedSlotIndexes {
		if index < 0 || index >= len(request.Slots) || indexes[index] {
			return nil, domain.ErrPrivateSelection
		}
		indexes[index] = true
	}
	selected := make([]domain.PrivateScheduleSlot, 0, len(indexes))
	for index, slot := range request.Slots {
		if indexes[index] {
			selected = append(selected, slot)
		}
	}
	return selected, nil
}
