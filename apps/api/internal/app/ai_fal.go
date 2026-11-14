package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// falProvider runs models on fal.ai's queue API (https://fal.ai/docs/model-apis/model-endpoints/queue):
// POST https://queue.fal.run/<model> with "Authorization: Key <AI_FAL_KEY>" answers request_id, status_url,
// response_url and cancel_url; status is IN_QUEUE, IN_PROGRESS or COMPLETED; the result is read from
// response_url; PUT cancel_url drops a queued request. Pictures go in as base64 data URIs.
//
// fal.ai documents no idempotency key: a submit whose answer was lost is never sent again (Idempotent false).
type falProvider struct{ aiBase }

func newFal(key, baseURL string) *falProvider {
	return &falProvider{newAIBase(AIProviderFal, "fal.ai", baseURL, "Key "+key)}
}

func (f *falProvider) Idempotent() bool     { return false }
func (f *falProvider) Supports(aiCall) bool { return true }

// falInput is the model's input, per operation (each model's API page).
func falInput(c aiCall) map[string]any {
	img := ""
	if len(c.Image) > 0 {
		img = dataURI(c.ImageType, c.Image)
	}
	switch c.Op {
	case AIGenerate:
		return map[string]any{"prompt": c.Prompt, "image_size": map[string]int{"width": c.Width, "height": c.Height},
			"num_images": 1, "output_format": "png", "enable_safety_checker": true}
	case AIEdit:
		return map[string]any{"prompt": c.Prompt, "image_url": img, "num_images": 1, "output_format": "png"}
	case AIInpaint:
		return map[string]any{"prompt": c.Prompt, "image_url": img, "mask_url": dataURI("image/png", c.Mask), "num_images": 1, "output_format": "png"}
	case AIErase:
		return map[string]any{"image_url": img, "mask_url": dataURI("image/png", c.Mask), "mask_type": "manual", "preserve_alpha": true}
	case AIExpand:
		return map[string]any{"image_url": img, "canvas_size": []int{c.CanvasWidth, c.CanvasHeight},
			"original_image_size": []int{c.PlaceWidth, c.PlaceHeight}, "original_image_location": []int{c.ImageX, c.ImageY},
			"prompt": c.Prompt}
	case AIRemoveBackground, AIUpscale:
		return map[string]any{"image_url": img}
	case AIReplaceBackground:
		return map[string]any{"image_url": img, "prompt": c.Prompt, "num_images": 1}
	case AIEnhance:
		in := map[string]any{"image_url": img, "upscale_factor": 2}
		if c.Prompt != "" {
			in["prompt"] = c.Prompt
		}
		return in
	case AITextToVideo:
		return map[string]any{"prompt": c.Prompt, "duration": strconv.Itoa(c.Duration), "aspect_ratio": c.AspectRatio}
	case AIImageToVideo:
		return map[string]any{"prompt": c.Prompt, "image_url": img, "duration": strconv.Itoa(c.Duration)}
	}
	return map[string]any{}
}

func (f *falProvider) Submit(ctx context.Context, model string, c aiCall, _ string) (aiRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, aiSubmitTimeout)
	defer cancel()
	var out struct {
		RequestID   string `json:"request_id"`
		StatusURL   string `json:"status_url"`
		ResponseURL string `json:"response_url"`
		CancelURL   string `json:"cancel_url"`
	}
	status, raw, err := f.call(ctx, http.MethodPost, f.base.String()+"/"+model, falInput(c), nil, &out)
	if err != nil {
		return aiRequest{}, err
	}
	if status >= 300 {
		return aiRequest{}, f.submitError(status, raw)
	}
	r := aiRequest{ID: out.RequestID, StatusURL: out.StatusURL, ResultURL: out.ResponseURL, CancelURL: out.CancelURL}
	if r.ID == "" || !f.sameOrigin(r.StatusURL) || !f.sameOrigin(r.ResultURL) || (r.CancelURL != "" && !f.sameOrigin(r.CancelURL)) {
		// It took the request (2xx) but the answer is unusable: it may run, so it is not sent anywhere else.
		return aiRequest{}, &aiError{Kind: aiUnknown, Provider: f.name, Status: status, Msg: "fal.ai answered without a usable request"}
	}
	return r, nil
}

func (f *falProvider) Check(ctx context.Context, r aiRequest) (aiProgress, error) {
	var st struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	status, _, err := f.call(ctx, http.MethodGet, r.StatusURL, nil, nil, &st)
	if err != nil {
		return aiProgress{}, err
	}
	if status >= 300 {
		return aiProgress{}, &aiError{Kind: aiUnknown, Provider: f.name, Status: status, Msg: "fal.ai did not say how the request is going"}
	}
	if st.Status != "COMPLETED" {
		return aiProgress{}, nil
	}
	var res struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Image *struct {
			URL string `json:"url"`
		} `json:"image"`
		Video *struct {
			URL string `json:"url"`
		} `json:"video"`
		HasNSFW []bool `json:"has_nsfw_concepts"`
	}
	status, raw, err := f.call(ctx, http.MethodGet, r.ResultURL, nil, nil, &res)
	if err != nil {
		return aiProgress{}, err
	}
	if status >= 300 || st.Error != "" {
		msg := aiDetail(raw)
		if msg == "" {
			msg = st.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("fal.ai answered %d", status)
		}
		return aiProgress{Done: true, Failed: true, Message: msg}, nil
	}
	if len(res.HasNSFW) > 0 && res.HasNSFW[0] {
		return aiProgress{Done: true, Failed: true, Refused: true, Message: "fal.ai's safety check held the result back"}, nil
	}
	switch {
	case len(res.Images) > 0 && res.Images[0].URL != "":
		return aiProgress{Done: true, OutputURL: res.Images[0].URL}, nil
	case res.Image != nil && res.Image.URL != "":
		return aiProgress{Done: true, OutputURL: res.Image.URL}, nil
	case res.Video != nil && res.Video.URL != "":
		return aiProgress{Done: true, OutputURL: res.Video.URL}, nil
	}
	return aiProgress{Done: true, Failed: true, Message: "fal.ai sent no result"}, nil
}

func (f *falProvider) Cancel(ctx context.Context, r aiRequest) (bool, error) {
	if r.CancelURL == "" {
		return false, nil
	}
	var out struct {
		Status string `json:"status"`
	}
	status, raw, err := f.call(ctx, http.MethodPut, r.CancelURL, nil, nil, &out)
	if err != nil {
		return false, err
	}
	if status >= 300 {
		_ = json.Unmarshal(raw, &out)
	}
	// 200 {"status":"OK"}: removed from the queue, it never runs. 202: it is running and may still finish.
	return status == http.StatusOK && out.Status == "OK", nil
}

// Forget: fal.ai keeps results on its CDN under the account's retention settings (deleting needs a key with
// assets:write); nothing is asked here. ponytail: set the account's media expiry in fal.ai's dashboard.
func (f *falProvider) Forget(context.Context, aiRequest) {}
