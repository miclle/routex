package service

import "strings"

// Soft Project policies use the immutable Project account identity, never a Key,
// creator or manager account. Hard historical rows keep their existing readers.
func projectMonthlyID(id string) bool {
	return strings.HasPrefix(id, "prj_") && len(id) > len("prj_") && safeTeamSessionID(id)
}

// Soft Project policy requires the retained exact Project identity from the
// same publication snapshot; no current manager or creator is substituted.
func runtimeProjectMonthlyIdentity(data *runtimeData, target string) bool {
	if !projectMonthlyID(target) || data.ProjectData == nil {
		return false
	}
	matches := 0
	for _, project := range data.ProjectData.Projects {
		if project.ID == target {
			if project.CreatedAt.IsZero() {
				return false
			}
			matches++
		}
	}
	return matches == 1
}
