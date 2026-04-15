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

package app

import (
	"reflect"
	"testing"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
)

func TestBootstrapTypeAliasesReuseBootstrapPackageTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		root      any
		bootstrap any
	}{
		{name: "coreInfrastructure", root: coreInfrastructure{}, bootstrap: appbootstrap.CoreInfrastructure{}},
		{name: "alarmModeComponents", root: alarmModeComponents{}, bootstrap: appbootstrap.AlarmModeComponents{}},
		{name: "alarmDependencies", root: alarmDependencies{}, bootstrap: appbootstrap.AlarmDependencies{}},
		{name: "scraperHolodexProfileFoundation", root: scraperHolodexProfileFoundation{}, bootstrap: appbootstrap.ScraperHolodexProfileFoundation{}},
		{name: "coreIntegrationServices", root: coreIntegrationServices{}, bootstrap: appbootstrap.CoreIntegrationServices{}},
		{name: "botDependencyModules", root: botDependencyModules{}, bootstrap: appbootstrap.BotDependencyModules{}},
		{name: "botWebhookRuntimeDependencies", root: botWebhookRuntimeDependencies{}, bootstrap: appbootstrap.BotWebhookRuntimeDependencies{}},
		{name: "botConfigSubscriberDependencies", root: botConfigSubscriberDependencies{}, bootstrap: appbootstrap.BotConfigSubscriberDependencies{}},
		{name: "botConfigSubscriberRuntimeDependencies", root: botConfigSubscriberRuntimeDependencies{}, bootstrap: appbootstrap.BotConfigSubscriberRuntimeDependencies{}},
		{name: "botAdminServerDependencies", root: botAdminServerDependencies{}, bootstrap: appbootstrap.AdminServerDependencies{}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if reflect.TypeOf(tc.root) != reflect.TypeOf(tc.bootstrap) {
				t.Fatalf("root type %v must reuse bootstrap type %v", reflect.TypeOf(tc.root), reflect.TypeOf(tc.bootstrap))
			}
		})
	}
}
