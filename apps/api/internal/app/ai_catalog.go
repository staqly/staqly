package app

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The AI catalog: which operations the editor and the media library offer, and which model of which provider
// runs each. This is data, kept apart from the code that runs jobs (ai.go) and the provider clients (ai_fal.go,
// ai_higgsfield.go). Model ids and request shapes follow the providers' API pages (fal.ai/models/<id>/api,
// docs.higgsfield.ai/docs/openapi.json); prices are the list prices those pages showed on 2026-10-26, kept as
// estimates (AI_PRICES overrides one; the provider's bill is what counts).
//
// An operation runs on the first configured provider that has a model for it (fal.ai first), and falls back to
// the next one only when the first did not take the request (it is down, refuses the server's key or is busy).
// A provider only has a model for an operation when that model does the same thing: no operation is ever swapped
// for another one.

// AI operations.
const (
	AIGenerate          = "generate"           // a picture from a prompt
	AIEdit              = "edit"               // change a picture as the prompt says
	AIInpaint           = "inpaint"            // generative fill: redraw a painted area as the prompt says
	AIErase             = "erase"              // remove what a painted area covers
	AIExpand            = "expand"             // outpainting: fill new space around a picture
	AIRemoveBackground  = "remove_background"  // cut the subject out (the editor's free on-device cut-out stays the default)
	AIReplaceBackground = "replace_background" // a new background from a prompt
	AIUpscale           = "upscale"            // more pixels, same picture
	AIEnhance           = "enhance"            // sharper, cleaner detail at twice the size
	AITextToVideo       = "text_to_video"      // a video clip from a prompt
	AIImageToVideo      = "image_to_video"     // a picture brought to life as a video clip
	AIWrite             = "write"              // a post or a thread from a description (POST /v1/ai/write, not a job)
)

// AI providers.
const (
	AIProviderFal        = "fal"
	AIProviderHiggsfield = "higgsfield"
)

// aiOpSpec is what an operation takes.
type aiOpSpec struct {
	ID     string
	Kind   string // "image", "video" or "text": what it makes
	Prompt string // "required", "optional" or "none"
	Image  bool   // needs a picture
	Mask   bool   // needs a mask of the same size (white: the area to change)
}

// aiOps in the order the editor lists them.
var aiOps = []aiOpSpec{
	{AIGenerate, "image", "required", false, false},
	{AIEdit, "image", "required", true, false},
	{AIInpaint, "image", "required", true, true},
	{AIErase, "image", "none", true, true},
	{AIExpand, "image", "optional", true, false},
	{AIRemoveBackground, "image", "none", true, false},
	{AIReplaceBackground, "image", "required", true, false},
	{AIUpscale, "image", "none", true, false},
	{AIEnhance, "image", "optional", true, false},
	{AITextToVideo, "video", "required", false, false},
	{AIImageToVideo, "video", "required", true, false},
	{AIWrite, "text", "required", false, false},
}

func aiOpByID(id string) (aiOpSpec, bool) {
	for _, o := range aiOps {
		if o.ID == id {
			return o, true
		}
	}
	return aiOpSpec{}, false
}

// Price units.
const (
	aiPerImage     = "image"     // one request
	aiPerMegapixel = "megapixel" // the result's pixels, rounded up to whole megapixels
	aiPerSecond    = "second"    // a video's length
)

// aiModel is one provider's model for one operation.
type aiModel struct {
	Provider string
	Op       string
	ID       string  // the provider's model id (fal.ai) or path (Higgsfield)
	Unit     string  // what Price is per
	Price    float64 // USD per Unit, an estimate
}

// aiCatalog: cheap, fast models for simple work (FLUX.1 schnell for new pictures, Recraft Crisp for upscaling),
// stronger ones where the task needs them. Higgsfield's public API has text-to-image (Soul) and Kling 2.5 Turbo
// video; the image editing operations are fal.ai only.
var aiCatalog = []aiModel{
	{AIProviderFal, AIGenerate, "fal-ai/flux/schnell", aiPerMegapixel, 0.003},
	{AIProviderFal, AIEdit, "fal-ai/flux-pro/kontext", aiPerImage, 0.04},
	{AIProviderFal, AIInpaint, "fal-ai/flux-pro/v1/fill", aiPerMegapixel, 0.05},
	{AIProviderFal, AIErase, "fal-ai/bria/eraser", aiPerImage, 0.04},
	{AIProviderFal, AIExpand, "fal-ai/bria/expand", aiPerImage, 0.04},
	{AIProviderFal, AIRemoveBackground, "fal-ai/bria/background/remove", aiPerImage, 0.018},
	{AIProviderFal, AIReplaceBackground, "fal-ai/bria/background/replace", aiPerImage, 0.04},
	{AIProviderFal, AIUpscale, "fal-ai/recraft/upscale/crisp", aiPerImage, 0.004},
	{AIProviderFal, AIEnhance, "fal-ai/clarity-upscaler", aiPerMegapixel, 0.03},
	{AIProviderFal, AITextToVideo, "fal-ai/kling-video/v2.5-turbo/pro/text-to-video", aiPerSecond, 0.07},
	{AIProviderFal, AIImageToVideo, "fal-ai/kling-video/v2.5-turbo/pro/image-to-video", aiPerSecond, 0.07},
	// Charged per token; a post or a thread with Gemini 2.5 Flash is well under a cent.
	{AIProviderFal, AIWrite, "openrouter/router", aiPerImage, 0.005},

	// Higgsfield publishes no list prices; these follow its documented estimate example and fal.ai's price of
	// the same Kling model.
	{AIProviderHiggsfield, AIGenerate, "higgsfield-ai/soul/standard", aiPerImage, 0.10},
	{AIProviderHiggsfield, AITextToVideo, "kling-video/v2.5-turbo/pro/text-to-video", aiPerSecond, 0.07},
	{AIProviderHiggsfield, AIImageToVideo, "kling-video/v2.5-turbo/pro/image-to-video", aiPerSecond, 0.07},
}

// ParseAIPrices reads AI_PRICES: "model=usd,model=usd" (USD per the model's unit), e.g.
// "fal-ai/flux/schnell=0.004". Unknown models are refused, so a typo is not silently ignored.
func ParseAIPrices(v string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, price, ok := strings.Cut(part, "=")
		id = strings.TrimSpace(id)
		p, err := strconv.ParseFloat(strings.TrimSpace(price), 64)
		if !ok || err != nil || p < 0 || math.IsInf(p, 0) || math.IsNaN(p) {
			return nil, fmt.Errorf("AI_PRICES: %q is not model=price", part)
		}
		known := false
		for _, m := range aiCatalog {
			known = known || m.ID == id
		}
		if !known {
			return nil, fmt.Errorf("AI_PRICES: %q is not a model staqly uses", id)
		}
		out[id] = p
	}
	return out, nil
}

// ParseAIOperations reads AI_OPERATIONS: the operations offered (comma-separated ids; empty = all).
func ParseAIOperations(v string) ([]string, error) {
	var out []string
	for _, id := range strings.Split(v, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := aiOpByID(id); !ok {
			return nil, fmt.Errorf("AI_OPERATIONS: %q is not an AI operation", id)
		}
		out = append(out, id)
	}
	return out, nil
}

// estimateCost: the request's estimated cost in millionths of a USD.
func (s *Service) estimateCost(m aiModel, c aiCall) int64 {
	price := m.Price
	if p, ok := s.Cfg.AI.Prices[m.ID]; ok {
		price = p
	}
	units := 1.0
	switch m.Unit {
	case aiPerMegapixel:
		w, h := c.outputSize()
		units = math.Max(1, math.Ceil(float64(w*h)/1e6))
	case aiPerSecond:
		units = float64(c.Duration)
	}
	return int64(math.Round(price * units * 1e6))
}

// outputSize is the result's expected size in pixels (for per-megapixel prices).
func (c aiCall) outputSize() (int, int) {
	switch c.Op {
	case AIGenerate:
		return c.Width, c.Height
	case AIEnhance:
		return c.ImageWidth * 2, c.ImageHeight * 2
	case AIExpand:
		return c.CanvasWidth, c.CanvasHeight
	}
	return c.ImageWidth, c.ImageHeight
}
