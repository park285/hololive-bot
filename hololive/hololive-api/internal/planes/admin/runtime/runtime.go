// Package runtime exposes the stable composition surface for the admin plane.
// Implementation details remain protected by the module's internal boundary.
package runtime

import app "github.com/kapu/hololive-api/internal/planes/admin/internal/app"

type Runtime = app.AdminAPIRuntime

var Build = app.BuildAdminAPIRuntime
