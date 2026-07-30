package render

import (
	"fmt"
	"strings"
)

func paginateHelpDocument(document helpCardDocument) ([]helpCardPage, error) {
	pages, err := fillHelpCardPages(document)
	if err != nil {
		return nil, err
	}

	if len(pages) == 0 || len(pages) > helpCardMaxImages {
		return nil, fmt.Errorf("help page count %d is outside 1..%d", len(pages), helpCardMaxImages)
	}
	for index := range pages {
		pages[index].subtitle = helpPageSubtitle(pages[index].sections, index+1, len(pages))
	}
	return pages, nil
}

func fillHelpCardPages(document helpCardDocument) ([]helpCardPage, error) {
	packer := newHelpPagePacker(document.title)
	for _, section := range document.sections {
		if section.height > helpCardMaxContentH {
			if err := packer.appendOversizedSection(section); err != nil {
				return nil, err
			}
			continue
		}
		packer.appendSection(section)
	}
	return packer.finish(), nil
}

type helpPagePacker struct {
	title         string
	pages         []helpCardPage
	current       []helpCardSection
	currentHeight int
}

func newHelpPagePacker(title string) helpPagePacker {
	return helpPagePacker{
		title:   title,
		pages:   make([]helpCardPage, 0, 3),
		current: make([]helpCardSection, 0, 4),
	}
}

func (p *helpPagePacker) appendSection(section helpCardSection) {
	if len(p.current) > 0 && p.currentHeight+section.height > helpCardTargetContentH {
		p.flush()
	}
	p.current = append(p.current, section)
	p.currentHeight += section.height
}

func (p *helpPagePacker) appendOversizedSection(section helpCardSection) error {
	p.flush()
	fragments, err := splitHelpSection(section)
	if err != nil {
		return err
	}
	for _, fragment := range fragments {
		p.pages = append(p.pages, newHelpCardPage(p.title, []helpCardSection{fragment}, fragment.height))
	}
	return nil
}

func (p *helpPagePacker) flush() {
	if len(p.current) == 0 {
		return
	}
	p.pages = append(p.pages, newHelpCardPage(p.title, p.current, p.currentHeight))
	p.current = nil
	p.currentHeight = 0
}

func (p *helpPagePacker) finish() []helpCardPage {
	p.flush()
	return p.pages
}

func splitHelpSection(section helpCardSection) ([]helpCardSection, error) {
	fragments := make([]helpCardSection, 0, 2)
	rows := section.rows
	continued := false
	for len(rows) > 0 {
		label := section.label
		if continued {
			label += " · 계속"
		}
		fragment := helpCardSection{label: label, height: helpCardSectionHeight}
		for len(rows) > 0 && fragment.height+rows[0].height <= helpCardMaxContentH {
			fragment.rows = append(fragment.rows, rows[0])
			fragment.height += rows[0].height
			rows = rows[1:]
		}
		if len(fragment.rows) == 0 {
			return nil, fmt.Errorf("help section %q contains a row larger than the page budget", section.label)
		}
		fragments = append(fragments, fragment)
		continued = true
	}
	return fragments, nil
}

func newHelpCardPage(title string, sections []helpCardSection, contentHeight int) helpCardPage {
	return helpCardPage{
		title:         title,
		sections:      append([]helpCardSection(nil), sections...),
		contentHeight: contentHeight,
	}
}

func helpPageSubtitle(sections []helpCardSection, page, total int) string {
	labels := make([]string, 0, len(sections))
	for _, section := range sections {
		label := strings.TrimSuffix(section.label, " · 계속")
		if len(labels) == 0 || labels[len(labels)-1] != label {
			labels = append(labels, label)
		}
	}
	subtitle := strings.Join(labels, " · ")
	if total > 1 {
		subtitle = fmt.Sprintf("%s · %d/%d", subtitle, page, total)
	}
	return subtitle
}
