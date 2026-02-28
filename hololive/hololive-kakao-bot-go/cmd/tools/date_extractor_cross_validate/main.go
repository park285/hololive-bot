package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kapu/hololive-shared/pkg/service/majorevent"
)

type fixtureFile struct {
	SchemaVersion uint32        `json:"schema_version"`
	GeneratedFrom string        `json:"generated_from"`
	Cases         []fixtureCase `json:"cases"`
}

type fixtureCase struct {
	Name          string   `json:"name"`
	InputHTML     string   `json:"input_html"`
	InputFile     string   `json:"input_file"`
	ExpectedDates []string `json:"expected_dates"`
}

type crossValidationResult struct {
	Name  string   `json:"name"`
	Dates []string `json:"dates"`
}

type crossValidationOutput struct {
	FixturePath string                  `json:"fixture_path"`
	Results     []crossValidationResult `json:"results"`
}

func main() {
	fixturePath := flag.String("fixture", "", "path to cross-validation fixture json")
	outputPath := flag.String("output", "", "output json path (default: stdout)")
	flag.Parse()

	if err := run(*fixturePath, *outputPath); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(fixturePath, outputPath string) error {
	if strings.TrimSpace(fixturePath) == "" {
		return errors.New("-fixture is required")
	}

	fixture, fixtureAbsPath, fixtureDir, err := loadFixture(fixturePath)
	if err != nil {
		return fmt.Errorf("load fixture: %w", err)
	}

	if fixture.SchemaVersion != 1 {
		return fmt.Errorf("unsupported fixture schema_version: %d", fixture.SchemaVersion)
	}
	if strings.TrimSpace(fixture.GeneratedFrom) == "" {
		return errors.New("fixture generated_from must not be empty")
	}
	if len(fixture.Cases) == 0 {
		return errors.New("fixture cases must not be empty")
	}

	extractor := majorevent.NewDateExtractor()
	results := make([]crossValidationResult, 0, len(fixture.Cases))

	for idx, testCase := range fixture.Cases {
		if err := validateCase(idx, testCase); err != nil {
			return err
		}

		html, err := resolveInputHTML(fixtureDir, testCase)
		if err != nil {
			return fmt.Errorf("resolve input html for case %q: %w", testCase.Name, err)
		}

		dates := extractor.ExtractEventDates(html)
		formatted := make([]string, 0, len(dates))
		for _, date := range dates {
			formatted = append(formatted, date.Format("2006-01-02"))
		}

		results = append(results, crossValidationResult{
			Name:  testCase.Name,
			Dates: formatted,
		})
	}

	output := crossValidationOutput{
		FixturePath: fixtureAbsPath,
		Results:     results,
	}
	if err := writeOutput(outputPath, output); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}

func loadFixture(fixturePath string) (fixtureFile, string, string, error) {
	fixtureAbsPath, err := filepath.Abs(fixturePath)
	if err != nil {
		return fixtureFile{}, "", "", fmt.Errorf("resolve absolute fixture path: %w", err)
	}

	body, err := os.ReadFile(fixtureAbsPath)
	if err != nil {
		return fixtureFile{}, "", "", fmt.Errorf("read fixture file: %w", err)
	}

	var fixture fixtureFile
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return fixtureFile{}, "", "", fmt.Errorf("decode fixture json: %w", err)
	}

	fixtureDir := filepath.Dir(fixtureAbsPath)
	return fixture, fixtureAbsPath, fixtureDir, nil
}

func validateCase(index int, testCase fixtureCase) error {
	if strings.TrimSpace(testCase.Name) == "" {
		return fmt.Errorf("case[%d] name must not be empty", index)
	}

	hasInlineHTML := strings.TrimSpace(testCase.InputHTML) != ""
	hasInputFile := strings.TrimSpace(testCase.InputFile) != ""
	if hasInlineHTML == hasInputFile {
		return fmt.Errorf("case[%d] %q must define exactly one of input_html or input_file", index, testCase.Name)
	}

	return nil
}

func resolveInputHTML(fixtureDir string, testCase fixtureCase) (string, error) {
	if strings.TrimSpace(testCase.InputHTML) != "" {
		return testCase.InputHTML, nil
	}

	fileName := strings.TrimSpace(testCase.InputFile)
	cleanName := filepath.Clean(fileName)
	if cleanName == "." || strings.HasPrefix(cleanName, "..") {
		return "", fmt.Errorf("invalid input_file path: %q", fileName)
	}

	resolvedPath := filepath.Join(fixtureDir, cleanName)
	resolvedAbsPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("resolve absolute input_file path: %w", err)
	}

	fixtureDirAbs, err := filepath.Abs(fixtureDir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute fixture dir: %w", err)
	}
	if !strings.HasPrefix(resolvedAbsPath, fixtureDirAbs+string(filepath.Separator)) && resolvedAbsPath != fixtureDirAbs {
		return "", fmt.Errorf("input_file escapes fixture directory: %q", fileName)
	}

	body, err := os.ReadFile(resolvedAbsPath)
	if err != nil {
		return "", fmt.Errorf("read input_file %q: %w", fileName, err)
	}

	return string(body), nil
}

func writeOutput(outputPath string, payload crossValidationOutput) error {
	var writer io.Writer = os.Stdout

	if strings.TrimSpace(outputPath) != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}

		file, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer func() { _ = file.Close() }()
		writer = file
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		return fmt.Errorf("encode output json: %w", err)
	}

	return nil
}
