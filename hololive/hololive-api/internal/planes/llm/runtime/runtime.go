// Package runtime exposes the stable composition surface for the LLM plane.
// Implementation details remain protected by the module's internal boundary.
package runtime

import app "github.com/kapu/hololive-api/internal/planes/llm/internal/app"

type Runtime = app.LLMSchedulerRuntime

var Build = app.BuildLLMSchedulerRuntime
