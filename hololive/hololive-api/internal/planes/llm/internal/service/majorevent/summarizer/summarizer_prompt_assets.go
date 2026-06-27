package summarizer

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"text/template"

	json "github.com/park285/shared-go/pkg/json"
)

//go:embed graduated_members.json prompts/*.tmpl
var promptAssetFS embed.FS

var promptAssetFiles = []string{
	"graduated_members.json",
	"prompts/domain_context_part1.tmpl",
	"prompts/domain_context_part2.tmpl",
	"prompts/monthly_system_prompt.tmpl",
	"prompts/weekly_system_prompt.tmpl",
}

type graduatedMember struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

type graduatedData struct {
	Graduated         map[string][]graduatedMember `json:"graduated"`
	AffiliateInactive []graduatedMember            `json:"affiliate_inactive"`
	DissolvedBranches []struct {
		Branch  string   `json:"branch"`
		Date    string   `json:"date"`
		Members []string `json:"members"`
	} `json:"dissolved_branches"`
}

var parsedGraduatedData graduatedData
var promptVersion = mustPromptAssetVersion()

type SummaryType string

const (
	SummaryTypeWeekly  SummaryType = "weekly"
	SummaryTypeMonthly SummaryType = "monthly"
)

type promptsResult struct {
	weekly  string
	monthly string
	err     error
}

type promptTemplates struct {
	domainContextPart1 string
	domainContextPart2 string
	weekly             *template.Template
	monthly            *template.Template
}

var initPrompts = sync.OnceValue(func() promptsResult {
	var r promptsResult

	graduatedMembersJSON, err := fs.ReadFile(promptAssetFS, "graduated_members.json")
	if err != nil {
		r.err = fmt.Errorf("read graduated_members.json: %w", err)
		return r
	}
	if unmarshalErr := json.Unmarshal(graduatedMembersJSON, &parsedGraduatedData); unmarshalErr != nil {
		r.err = fmt.Errorf("parse graduated_members.json: %w", unmarshalErr)
		return r
	}

	templates, err := loadPromptTemplates()
	if err != nil {
		r.err = err
		return r
	}

	domainContext := templates.domainContextPart1 + buildMemberFilterSection() + "\n\n" + templates.domainContextPart2
	r.weekly, err = renderPromptTemplate(templates.weekly, domainContext)
	if err != nil {
		r.err = err
		return r
	}
	r.monthly, err = renderPromptTemplate(templates.monthly, domainContext)
	if err != nil {
		r.err = err
		return r
	}

	return r
})

func loadPromptTemplates() (promptTemplates, error) {
	domainContextPart1, err := readPromptAssetString("prompts/domain_context_part1.tmpl")
	if err != nil {
		return promptTemplates{}, err
	}
	domainContextPart2, err := readPromptAssetString("prompts/domain_context_part2.tmpl")
	if err != nil {
		return promptTemplates{}, err
	}
	weeklyText, err := readPromptAssetString("prompts/weekly_system_prompt.tmpl")
	if err != nil {
		return promptTemplates{}, err
	}
	monthlyText, err := readPromptAssetString("prompts/monthly_system_prompt.tmpl")
	if err != nil {
		return promptTemplates{}, err
	}

	weekly, err := template.New("weekly_system_prompt.tmpl").Parse(weeklyText)
	if err != nil {
		return promptTemplates{}, fmt.Errorf("parse weekly_system_prompt.tmpl: %w", err)
	}
	monthly, err := template.New("monthly_system_prompt.tmpl").Parse(monthlyText)
	if err != nil {
		return promptTemplates{}, fmt.Errorf("parse monthly_system_prompt.tmpl: %w", err)
	}

	return promptTemplates{
		domainContextPart1: domainContextPart1,
		domainContextPart2: domainContextPart2,
		weekly:             weekly,
		monthly:            monthly,
	}, nil
}

func readPromptAssetString(path string) (string, error) {
	content, err := fs.ReadFile(promptAssetFS, path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(content), nil
}

func renderPromptTemplate(tmpl *template.Template, domainContext string) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		DomainContext string
	}{DomainContext: domainContext}); err != nil {
		return "", fmt.Errorf("render %s: %w", tmpl.Name(), err)
	}
	return buf.String(), nil
}

func mustPromptAssetVersion() string {
	assets := make(map[string][]byte, len(promptAssetFiles))
	for _, path := range promptAssetFiles {
		content, err := fs.ReadFile(promptAssetFS, path)
		if err != nil {
			panic(fmt.Errorf("read %s for prompt version: %w", path, err))
		}
		assets[path] = content
	}
	return buildPromptAssetVersion(assets)
}

func buildPromptAssetVersion(assets map[string][]byte) string {
	paths := make([]string, 0, len(assets))
	for path := range assets {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	hasher := sha256.New()
	for _, path := range paths {
		hasher.Write([]byte(path))
		hasher.Write([]byte{0})
		hasher.Write(assets[path])
		hasher.Write([]byte{0})
	}

	checksum := hasher.Sum(nil)
	return hex.EncodeToString(checksum[:8])
}

func buildMemberFilterSection() string {
	var sb strings.Builder
	sb.WriteString(`<member_filter>
  Remove graduated or retired members from the "members" output field.
  Known graduated/retired members (reverse-chronological):
`)
	writeGraduatedBranches(&sb)
	writeAffiliateInactiveMembers(&sb)
	writeDissolvedBranches(&sb)

	sb.WriteString(`  If unsure about a member's status, keep them in the list.
  <large_group>
    If >8 participating members after filtering: abbreviate as "JP/EN 다수" or "전체 참여" — do NOT list individually.
    Exception: unit events (holoX, ReGLOSS, FLOW GLOW, etc.) — always list all unit members.
  </large_group>
</member_filter>`)
	return sb.String()
}

func writeGraduatedBranches(sb *strings.Builder) {
	branchOrder := []string{"JP", "EN", "DEV_IS", "HOLOSTARS_EN", "HOLOSTARS_JP"}
	for _, branch := range branchOrder {
		members := parsedGraduatedData.Graduated[branch]
		if len(members) == 0 {
			continue
		}
		sb.WriteString("    ")
		sb.WriteString(branch)
		sb.WriteString(": ")
		writeDatedMembers(sb, members)
		sb.WriteByte('\n')
	}
}

func writeAffiliateInactiveMembers(sb *strings.Builder) {
	if len(parsedGraduatedData.AffiliateInactive) == 0 {
		return
	}
	sb.WriteString("  Affiliate (inactive — remove from regular event listings):\n    ")
	writeDatedMembers(sb, parsedGraduatedData.AffiliateInactive)
	sb.WriteByte('\n')
}

func writeDatedMembers(sb *strings.Builder, members []graduatedMember) {
	for i, m := range members {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(m.Name)
		sb.WriteString(" (")
		sb.WriteString(m.Date)
		sb.WriteByte(')')
	}
}

func writeDissolvedBranches(sb *strings.Builder) {
	for _, d := range parsedGraduatedData.DissolvedBranches {
		sb.WriteString("  ")
		sb.WriteString(d.Branch)
		sb.WriteString(": entire branch dissolved (")
		sb.WriteString(d.Date)
		sb.WriteString(") — ")
		sb.WriteString(strings.Join(d.Members, ", "))
		sb.WriteByte('\n')
	}
}

func getDomainContext() string {
	if r := initPrompts(); r.err != nil {
		return ""
	}
	templates, err := loadPromptTemplates()
	if err != nil {
		return ""
	}
	return templates.domainContextPart1 + buildMemberFilterSection() + "\n\n" + templates.domainContextPart2
}

func getSystemPrompt(summaryType SummaryType) (string, error) {
	r := initPrompts()
	if r.err != nil {
		return "", fmt.Errorf("system prompt init: %w", r.err)
	}
	switch summaryType {
	case SummaryTypeMonthly:
		return r.monthly, nil
	case SummaryTypeWeekly:
		return r.weekly, nil
	default:
		return r.weekly, nil
	}
}
