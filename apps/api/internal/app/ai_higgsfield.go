package app

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"net/url"
)

// higgsfieldProvider runs models on Higgsfield's API (https://docs.higgsfield.ai, OpenAPI at
// docs.higgsfield.ai/docs/openapi.json): POST https://api.higgsfield.ai/<model> with
// "Authorization: Key <key id>:<key secret>" and an Idempotency-Key answers request_id, status_url and
// cancel_url; status is queued, in_progress, completed, failed, nsfw or canceled; a completed request carries
// images[].url or video.url. Failed and nsfw requests are not charged; POST cancel_url drops a queued one.
// Input pictures are uploaded first (POST /files/generate-upload-url, then PUT to the URL it gives).
//
// Its public API has text-to-image (Soul) and Kling 2.5 Turbo text-to-video and image-to-video; it is the
// fallback for those operations only.
type higgsfieldProvider struct{ aiBase }

func newHiggsfield(key, baseURL string) *higgsfieldProvider {
	return &higgsfieldProvider{newAIBase(AIProviderHiggsfield, "Higgsfield", baseURL, "Key "+key)}
}

func (h *higgsfieldProvider) Idempotent() bool { return true }

// Supports: Kling's text-to-video here takes no aspect ratio (it makes 16:9), so other shapes stay on fal.ai.
func (h *higgsfieldProvider) Supports(c aiCall) bool {
	return c.Op != AITextToVideo || c.AspectRatio == "" || c.AspectRatio == "16:9"
}

// soulRatios are Soul's aspect ratios; generate takes the closest one to the size asked for.
var soulRatios = []struct {
	name string
	r    float64
}{{"1:1", 1}, {"4:3", 4.0 / 3}, {"3:4", 3.0 / 4}, {"3:2", 1.5}, {"2:3", 2.0 / 3}, {"5:4", 1.25}, {"4:5", 0.8},
	{"16:9", 16.0 / 9}, {"9:16", 9.0 / 16}, {"21:9", 21.0 / 9}}

func closestSoulRatio(w, h int) string {
	best, d := "1:1", 1e9
	r := float64(w) / float64(max(h, 1))
	for _, s := range soulRatios {
		if x := math.Abs(s.r - r); x < d {
			best, d = s.name, x
		}
	}
	return best
}

// upload puts an input picture where Higgsfield can read it and answers its public URL.
func (h *higgsfieldProvider) upload(ctx context.Context, ct string, data []byte) (string, error) {
	var out struct {
		UploadURL     string            `json:"upload_url"`
		UploadHeaders map[string]string `json:"upload_headers"`
		PublicURL     string            `json:"public_url"`
	}
	status, raw, err := h.call(ctx, http.MethodPost, h.base.String()+"/files/generate-upload-url", map[string]string{"content_type": ct}, nil, &out)
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", h.submitError(status, raw)
	}
	if !h.fileURL(out.UploadURL) || !h.fileURL(out.PublicURL) {
		return "", &aiError{Kind: aiNotAccepted, Provider: h.name, Msg: "Higgsfield gave no usable upload address"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, out.UploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	for k, v := range out.UploadHeaders {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", ct)
	}
	// The storage URL is signed: Higgsfield's credentials never go there.
	res, err := aiDownloadHTTP.Do(req)
	if err != nil {
		return "", &aiError{Kind: aiNotAccepted, Provider: h.name, Msg: "the picture could not be uploaded to Higgsfield"}
	}
	_ = res.Body.Close()
	if res.StatusCode >= 300 {
		return "", &aiError{Kind: aiNotAccepted, Provider: h.name, Status: res.StatusCode, Msg: "the picture could not be uploaded to Higgsfield"}
	}
	return out.PublicURL, nil
}

func (h *higgsfieldProvider) Submit(ctx context.Context, model string, c aiCall, idemKey string) (aiRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, aiSubmitTimeout)
	defer cancel()
	var in map[string]any
	switch c.Op {
	case AIGenerate:
		in = map[string]any{"prompt": c.Prompt, "num_images": 1, "aspect_ratio": closestSoulRatio(c.Width, c.Height)}
	case AITextToVideo:
		in = map[string]any{"prompt": c.Prompt, "duration": c.Duration}
	case AIImageToVideo:
		u, err := h.upload(ctx, c.ImageType, c.Image)
		if err != nil {
			return aiRequest{}, err
		}
		in = map[string]any{"prompt": c.Prompt, "image_url": u, "duration": c.Duration}
	default:
		return aiRequest{}, &aiError{Kind: aiNotAccepted, Provider: h.name, Msg: "Higgsfield cannot do this"}
	}
	var out struct {
		RequestID string `json:"request_id"`
		StatusURL string `json:"status_url"`
		CancelURL string `json:"cancel_url"`
	}
	status, raw, err := h.call(ctx, http.MethodPost, h.base.String()+"/"+model, in, map[string]string{"Idempotency-Key": idemKey}, &out)
	if err != nil {
		return aiRequest{}, err
	}
	if status >= 300 {
		return aiRequest{}, h.submitError(status, raw)
	}
	r := aiRequest{ID: out.RequestID, StatusURL: out.StatusURL, CancelURL: out.CancelURL}
	if r.StatusURL == "" && r.ID != "" {
		r.StatusURL = h.base.String() + "/requests/" + url.PathEscape(r.ID) + "/status"
	}
	if r.ID == "" || !h.sameOrigin(r.StatusURL) || (r.CancelURL != "" && !h.sameOrigin(r.CancelURL)) {
		return aiRequest{}, &aiError{Kind: aiUnknown, Provider: h.name, Status: status, Msg: "Higgsfield answered without a usable request"}
	}
	return r, nil
}

func (h *higgsfieldProvider) Check(ctx context.Context, r aiRequest) (aiProgress, error) {
	var st struct {
		Status string  `json:"status"`
		Error  *string `json:"error"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Video *struct {
			URL string `json:"url"`
		} `json:"video"`
	}
	status, _, err := h.call(ctx, http.MethodGet, r.StatusURL, nil, nil, &st)
	if err != nil {
		return aiProgress{}, err
	}
	if status >= 300 {
		return aiProgress{}, &aiError{Kind: aiUnknown, Provider: h.name, Status: status, Msg: "Higgsfield did not say how the request is going"}
	}
	switch st.Status {
	case "completed":
		if len(st.Images) > 0 && st.Images[0].URL != "" {
			return aiProgress{Done: true, OutputURL: st.Images[0].URL}, nil
		}
		if st.Video != nil && st.Video.URL != "" {
			return aiProgress{Done: true, OutputURL: st.Video.URL}, nil
		}
		return aiProgress{Done: true, Failed: true, Message: "Higgsfield sent no result"}, nil
	case "failed":
		msg := "Higgsfield could not make it"
		if st.Error != nil && *st.Error != "" {
			msg = truncate(*st.Error, 300)
		}
		return aiProgress{Done: true, Failed: true, Message: msg}, nil
	case "nsfw":
		return aiProgress{Done: true, Failed: true, Refused: true, Message: "Higgsfield's content check refused it"}, nil
	case "canceled":
		return aiProgress{Done: true, Failed: true, Message: "Higgsfield cancelled the request"}, nil
	}
	return aiProgress{}, nil
}

func (h *higgsfieldProvider) Cancel(ctx context.Context, r aiRequest) (bool, error) {
	if r.CancelURL == "" {
		return false, nil
	}
	status, _, err := h.call(ctx, http.MethodPost, r.CancelURL, nil, nil, nil)
	if err != nil {
		return false, err
	}
	// 202: cancelled while queued (refunded); 400: it already started and runs to the end.
	return status == http.StatusAccepted, nil
}

// Forget deletes the request's files and parameters at Higgsfield once the result is downloaded.
func (h *higgsfieldProvider) Forget(ctx context.Context, r aiRequest) {
	if r.ID == "" {
		return
	}
	_, _, _ = h.call(ctx, http.MethodDelete, h.base.String()+"/requests/"+url.PathEscape(r.ID), nil, nil, nil)
}
