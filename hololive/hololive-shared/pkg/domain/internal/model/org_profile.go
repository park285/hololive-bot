// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package model

type TalentProfile struct {
	Slug         string               `json:"slug"`
	EnglishName  string               `json:"english_name"`
	JapaneseName string               `json:"japanese_name"`
	Catchphrase  string               `json:"catchphrase"`
	Description  string               `json:"description"`
	DataEntries  []TalentProfileEntry `json:"data_entries"`
	SocialLinks  []TalentSocialLink   `json:"social_links"`
	OfficialURL  string               `json:"official_url"`
}

type TalentProfileEntry struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type TalentSocialLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Translated struct {
	DisplayName string                     `json:"display_name"`
	Catchphrase string                     `json:"catchphrase"`
	Summary     string                     `json:"summary"`
	Highlights  []string                   `json:"highlights"`
	Data        []TranslatedProfileDataRow `json:"data"`
}

type TranslatedProfileDataRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
