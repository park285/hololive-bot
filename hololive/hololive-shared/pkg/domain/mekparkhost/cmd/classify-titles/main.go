package main

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

type inputTitle struct {
	VideoID   string `json:"video_id"`
	ChannelID string `json:"channel_id"`
	Title     string `json:"title"`
}

type classifiedTitle struct {
	VideoID string   `json:"video_id"`
	Unit    string   `json:"unit"`
	Hosts   []string `json:"hosts"`
	Guests  []string `json:"guests"`
}

func main() {
	if err := classifyTitles(os.Stdin, os.Stdout); err != nil {
		slog.Error("classify title corpus", "error", err)
		os.Exit(1)
	}
}

func classifyTitles(input io.Reader, output io.Writer) error {
	var titles []inputTitle

	if err := jsonv2.UnmarshalRead(input, &titles); err != nil {
		return fmt.Errorf("read title corpus: %w", err)
	}

	results := make([]classifiedTitle, 0, len(titles))
	for _, title := range titles {
		result := mekparkhost.Identify(title.ChannelID, title.Title)
		row := classifiedTitle{VideoID: title.VideoID, Unit: result.Unit}

		for _, person := range result.Hosts {
			row.Hosts = append(row.Hosts, person.ID)
		}

		for _, person := range result.Guests {
			row.Guests = append(row.Guests, person.ID)
		}

		results = append(results, row)
	}

	if err := jsonv2.MarshalWrite(output, results); err != nil {
		return fmt.Errorf("write title classifications: %w", err)
	}

	return nil
}
