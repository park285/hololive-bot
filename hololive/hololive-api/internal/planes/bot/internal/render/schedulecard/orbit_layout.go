package schedulecard

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	orbitTop      = 247.0
	orbitBottom   = 1386.0
	orbitCapacity = orbitBottom - orbitTop
	orbitTitleX   = 262.0
	orbitRight    = 1016.0
	orbitPad      = 12.0
	orbitRowGap   = 19.0
	orbitTitleLH  = 58.0
	orbitDetailLH = 44.0

	orbitContinued = "방송 내용 계속"
)

var (
	orbitTitleText  = txt{Size: 49, Face: faceXBold}
	orbitDetailText = txt{Size: 36, Face: faceRegular}
	orbitClockText  = txt{Size: 48, Face: faceBold}
)

type orbitPage struct{ groups []orbitGroup }

type orbitGroup struct {
	date         time.Time
	rows         []orbitRow
	start, total int
	mixed        bool
	y, h         float64
}

type orbitRow struct {
	title, detail []string
	label         string
	kind          timeKind
	status        Status
	marked        bool
	ordinal       int
	y, h          float64
}

func orbitWrap(f *fontSet, t txt, s string, first, next float64) []string {
	var lines []string

	remaining := f.width(t, s)
	space := f.width(t, " ")

	for limit := first; s != ""; limit = next {
		if remaining <= limit {
			return append(lines, s)
		}

		end, word, balanced, clause := len(s), 0, 0, 0
		w, best := 0.0, math.Inf(1)

		for i, r := range s {
			if r == ' ' {
				word = i

				tail := remaining - w - space

				if cost := math.Abs(w/limit - tail/next); tail <= next && cost < best {
					balanced, best = word, cost
				}
			}

			w += f.glyph(t.Face, r).adv * t.Size
			if w > limit {
				end = i

				break
			}

			if strings.ContainsRune("·/|—", r) {
				word = i + utf8.RuneLen(r)

				tail := remaining - w

				if strings.HasPrefix(s[word:], " ") {
					tail -= space
				}

				if tail <= next {
					clause = word
				}
			}
		}

		switch {
		case clause > 0:
			end = clause
		case balanced > 0:
			end = balanced
		case word > 0:
			end = word
		}

		lines = append(lines, strings.TrimRight(s[:end], " "))

		tail := strings.TrimLeft(s[end:], " ")

		remaining -= f.width(t, s[:len(s)-len(tail)])

		s = tail
	}

	return lines
}

func orbitRowHeight(title, detail int) float64 {
	h := float64(title) * orbitTitleLH

	if detail != 0 {
		h += 4 + float64(detail)*orbitDetailLH
	}

	return h
}

func measureOrbitRow(f *fontSet, s slot, ordinal int) orbitRow {
	label, kind := timeLabel(s)
	width := orbitRight - orbitTitleX
	first := width - 8

	if label != "" {
		first = width - max(151, f.width(orbitClockText, label)) - 24
	}

	title := orbitWrap(f, orbitTitleText, s.main, first, width-8)
	sub := strings.TrimSuffix(strings.TrimPrefix(s.sub, "("), ")")

	if len(s.tags) != 0 {
		if sub != "" {
			sub += " · "
		}

		sub += strings.Join(s.tags, " · ")
	}

	detail := orbitWrap(f, orbitDetailText, sub, width-12, width-12)

	return orbitRow{
		title: title, detail: detail, label: label, kind: kind, status: s.status, marked: s.highlight, ordinal: ordinal,
		h: orbitRowHeight(len(title), len(detail)),
	}
}

func (r orbitRow) lines() int { return len(r.title) + len(r.detail) }

func (r orbitRow) segment(from, to int) orbitRow {
	titles := len(r.title)

	r.title = r.title[min(from, titles):min(to, titles)]
	r.detail = r.detail[max(0, from-titles):max(0, to-titles)]

	if from > 0 {
		r.title = append([]string{orbitContinued}, r.title...)
	}

	r.h = orbitRowHeight(len(r.title), len(r.detail))

	return r
}

type orbitPiece struct {
	day      *orbitGroup
	lo, hi   int
	from, to int
	content  float64
}

func (p orbitPiece) height() float64 {
	minimum := 111.0

	if p.hi > p.lo && (p.day.rows[p.lo].ordinal != 0 || p.day.rows[p.hi-1].ordinal+1 != p.day.total) {
		minimum = 194
	}

	h := p.content + 2*orbitPad
	if p.from > 0 {
		h += orbitTitleLH

		if p.from > len(p.day.rows[p.lo].title) {
			h += 4
		}
	}

	return max(minimum, h)
}

func (p orbitPiece) join(q orbitPiece) orbitPiece {
	if p.hi == q.lo {
		p.content += orbitRowGap
	}

	p.hi, p.to = q.hi, q.to

	p.content += q.content

	return p
}

func (p orbitPiece) group() orbitGroup {
	g := *p.day

	g.rows, g.h = g.rows[p.lo:p.hi], p.height()

	if len(g.rows) == 0 {
		return g
	}

	g.start = g.rows[0].ordinal

	last := len(g.rows) - 1

	if p.from != 0 || p.to != g.rows[last].lines() {
		rows := make([]orbitRow, len(g.rows))
		copy(rows, g.rows)

		g.rows = rows

		if last == 0 {
			g.rows[0] = g.rows[0].segment(p.from, p.to)
		} else {
			g.rows[0] = g.rows[0].segment(p.from, g.rows[0].lines())
			g.rows[last] = g.rows[last].segment(0, p.to)
		}
	}

	return g
}

func orbitPieces(f *fontSet, days []day) []orbitPiece {
	pieces := make([]orbitPiece, 0, len(days))
	for _, d := range days {
		g := &orbitGroup{date: d.date, total: len(d.slots), rows: make([]orbitRow, 0, len(d.slots))}
		for i, s := range d.slots {
			g.mixed = g.mixed || s.status == StatusOff || s.status == StatusCancelled
			g.rows = append(g.rows, measureOrbitRow(f, s, i))
		}

		whole := orbitPiece{day: g, hi: len(g.rows), content: float64(max(0, len(g.rows)-1)) * orbitRowGap}
		for _, r := range g.rows {
			whole.content += r.h
		}

		if whole.hi > 0 {
			whole.to = g.rows[whole.hi-1].lines()
		}

		if whole.height() <= orbitCapacity {
			pieces = append(pieces, whole)

			continue
		}

		for i, row := range g.rows {
			p := orbitPiece{day: g, lo: i, hi: i + 1, to: row.lines(), content: row.h}
			if p.height() <= orbitCapacity {
				pieces = append(pieces, p)

				continue
			}

			for k := range row.lines() {
				h := orbitTitleLH

				if k >= len(row.title) {
					h = orbitDetailLH

					if k == len(row.title) {
						h += 4
					}
				}

				p.from, p.to, p.content = k, k+1, h
				pieces = append(pieces, p)
			}
		}
	}

	return pieces
}

func layoutOrbit(pieces []orbitPiece) []orbitPage {
	type cut struct {
		pages, breaks, next int
		penalty             float64
	}

	best := make([]cut, len(pieces)+1)
	for i := len(pieces) - 1; i >= 0; i-- {
		best[i].pages = len(best)

		var current orbitPiece

		used := 0.0

		for j := i; j < len(pieces); j++ {
			p := pieces[j]
			if current.day == p.day {
				used -= current.height()

				current = current.join(p)
			} else {
				current = p
			}

			used += current.height()

			spare := orbitCapacity - used

			if spare < 0 {
				if used-current.height()+current.content+2*orbitPad > orbitCapacity {
					break
				}

				continue
			}

			tail := best[j+1]
			n, cost := tail.pages+1, tail.penalty+spare*spare
			breaks := tail.breaks

			if p.hi > 0 && p.to < p.day.rows[p.hi-1].lines() {
				breaks++
			}

			if n < best[i].pages || n == best[i].pages && (breaks < best[i].breaks || breaks == best[i].breaks && cost < best[i].penalty) {
				best[i] = cut{pages: n, breaks: breaks, next: j + 1, penalty: cost}
			}
		}
	}

	pages := make([]orbitPage, 0, best[0].pages)
	for i := 0; i < len(pieces); i = best[i].next {
		pages = append(pages, positionOrbitPage(pieces[i:best[i].next]))
	}

	return pages
}

func positionOrbitPage(pieces []orbitPiece) orbitPage {
	page := orbitPage{groups: make([]orbitGroup, 0, min(len(pieces), daysPerWeek))}
	spare, units := orbitCapacity, 0.0

	for len(pieces) > 0 {
		p := pieces[0]

		pieces = pieces[1:]

		for len(pieces) > 0 && p.day == pieces[0].day {
			p = p.join(pieces[0])
			pieces = pieces[1:]
		}

		g := p.group()

		page.groups = append(page.groups, g)

		spare -= g.h

		units += float64(len(g.rows)) + .75
	}

	unit := min(43, spare/units)
	padding := min(32, (spare-unit*units)/float64(len(page.groups)))
	y := orbitTop

	for i := range page.groups {
		g := &page.groups[i]

		g.h += unit*(float64(len(g.rows))+.75) + padding

		g.y = y

		gap := orbitRowGap + unit*.65
		inner := gap * float64(max(0, len(g.rows)-1))

		for _, r := range g.rows {
			inner += r.h
		}

		rowY := y + (g.h-inner)/2
		for j := range g.rows {
			g.rows[j].y = rowY

			rowY += g.rows[j].h + gap
		}

		y += g.h
	}

	return page
}
