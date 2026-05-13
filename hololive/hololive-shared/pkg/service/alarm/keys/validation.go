package keys

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type ChannelContentAlarmTargetDefinition struct {
	OwnerLabel string
	ChannelID  string
}

func ValidateChannelContentAlarmTargetDefinitions(definitions []ChannelContentAlarmTargetDefinition) error {
	missing := make([]string, 0)
	duplicates := make([]string, 0)
	seenTargets := make(map[string]string, len(definitions)*2)

	for idx, definition := range definitions {
		definitionMissing, definitionDuplicates := validateChannelContentAlarmTargetDefinition(idx, definition, seenTargets)
		missing = append(missing, definitionMissing...)
		duplicates = append(duplicates, definitionDuplicates...)
	}

	if len(missing) == 0 && len(duplicates) == 0 {
		return nil
	}

	sort.Strings(missing)
	sort.Strings(duplicates)

	issues := make([]string, 0, 2)
	if len(missing) > 0 {
		issues = append(issues, "missing operating channel targets for "+strings.Join(missing, ", "))
	}
	if len(duplicates) > 0 {
		issues = append(issues, "duplicate deployment targets: "+strings.Join(duplicates, "; "))
	}

	return errors.New(strings.Join(issues, "; "))
}

func validateChannelContentAlarmTargetDefinition(idx int, definition ChannelContentAlarmTargetDefinition, seenTargets map[string]string) ([]string, []string) {
	ownerLabel := normalizedTargetDefinitionOwner(definition.OwnerLabel, idx)
	channelID := strings.TrimSpace(definition.ChannelID)
	if channelID == "" {
		return []string{ownerLabel}, nil
	}

	missing := make([]string, 0)
	duplicates := make([]string, 0)
	targets := BuildChannelContentAlarmTargetKeys(channelID)
	targetEntries := []struct {
		label string
		key   string
	}{
		{label: "community", key: strings.TrimSpace(targets.CommunitySubscribersKey)},
		{label: "shorts", key: strings.TrimSpace(targets.ShortsSubscribersKey)},
	}

	seenWithinDefinition := make(map[string]string, len(targetEntries))
	for _, entry := range targetEntries {
		if entry.key == "" {
			missing = append(missing, ownerLabel+":"+entry.label)
			continue
		}

		targetOwner := ownerLabel + ":" + entry.label
		if previousLabel, exists := seenWithinDefinition[entry.key]; exists {
			duplicates = append(duplicates, fmt.Sprintf("%s duplicates %s (%s)", targetOwner, previousLabel, entry.key))
			continue
		}
		seenWithinDefinition[entry.key] = targetOwner

		if previousOwner, exists := seenTargets[entry.key]; exists {
			duplicates = append(duplicates, fmt.Sprintf("%s duplicates %s (%s)", targetOwner, previousOwner, entry.key))
			continue
		}
		seenTargets[entry.key] = targetOwner
	}

	return missing, duplicates
}

func normalizedTargetDefinitionOwner(ownerLabel string, idx int) string {
	trimmed := strings.TrimSpace(ownerLabel)
	if trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("definition[%d]", idx+1)
}
