package friend

import (
	"encoding/json"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	friendsvc "github.com/applemango/chain/internal/service/friend"
)

// LocationShareController は相手ごとの位置共有・エリア（07 §4）。
type LocationShareController struct {
	Shares *friendsvc.LocationShareService
}

func (h *LocationShareController) Register(r fiber.Router) {
	r.Put("/friends/:did/location-share", h.setShare)
	r.Get("/friends/locations", h.listLocations)
	r.Put("/me/location-sharing", h.setPaused)
	r.Get("/me/areas", h.listAreas)
	r.Post("/me/areas", h.createArea)
	r.Patch("/me/areas/:id", h.updateArea)
	r.Delete("/me/areas/:id", h.deleteArea)
}

type locationShareRequest struct {
	Precision string `json:"precision"`
	AreasOnly bool   `json:"areasOnly"`
}

func (h *LocationShareController) setShare(c fiber.Ctx) error {
	did, err := syntax.ParseDID(c.Params("did"))
	if err != nil {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"did": "DID の形式が不正です"})
	}
	var req locationShareRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	if err := h.Shares.SetPrecision(c.Context(), httpx.DID(c), did.String(), req.Precision, req.AreasOnly); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type locationSharingRequest struct {
	Paused *bool `json:"paused"`
}

func (r *locationSharingRequest) Validate() map[string]string {
	if r.Paused == nil {
		return map[string]string{"paused": "必須です"}
	}
	return nil
}

func (h *LocationShareController) setPaused(c fiber.Ctx) error {
	var req locationSharingRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	if err := h.Shares.SetPaused(c.Context(), httpx.DID(c), *req.Paused); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type areaLabelResponse struct {
	Label string `json:"label"`
}

// friendLocationResponse は精度ごとに形が変わるので、含める項目を自分で組み立てる（07 §4.2）。
type friendLocationResponse struct {
	DID            string
	Precision      string
	Lat            float64
	Lon            float64
	AccuracyM      float64
	Area           *friendsvc.AreaLabel
	Outside        bool
	Direction      string
	DistanceBucket string
	ViewerStale    bool
	UpdatedAt      *time.Time
	Stale          bool
}

func (r friendLocationResponse) MarshalJSON() ([]byte, error) {
	out := map[string]any{"did": r.DID, "precision": r.Precision}
	if r.Stale {
		out["stale"] = true
		return json.Marshal(out)
	}
	if r.Precision == friendsvc.PrecisionOff {
		return json.Marshal(out)
	}
	if r.UpdatedAt != nil {
		out["updatedAt"] = *r.UpdatedAt
	}
	switch {
	case r.Precision == friendsvc.PrecisionExact:
		out["lat"] = r.Lat
		out["lon"] = r.Lon
		out["accuracyM"] = r.AccuracyM
	case r.Precision == friendsvc.PrecisionApprox:
		out["lat"] = r.Lat
		out["lon"] = r.Lon
	case r.Precision == friendsvc.PrecisionArea:
		if r.Area != nil {
			out["area"] = areaLabelResponse{Label: r.Area.Label}
		} else {
			out["area"] = nil
			out["outside"] = r.Outside
		}
	case r.Precision == friendsvc.PrecisionDirection:
		if r.ViewerStale {
			out["viewerStale"] = true
		} else {
			out["direction"] = r.Direction
			out["distanceBucket"] = r.DistanceBucket
		}
	}
	return json.Marshal(out)
}

func friendLocationResponseFrom(f friendsvc.FriendLocation) friendLocationResponse {
	return friendLocationResponse{
		DID: f.DID, Precision: f.Precision, Lat: f.Lat, Lon: f.Lon, AccuracyM: f.AccuracyM,
		Area: f.Area, Outside: f.Outside, Direction: f.Direction, DistanceBucket: f.DistanceBucket,
		ViewerStale: f.ViewerStale, UpdatedAt: f.UpdatedAt, Stale: f.Stale,
	}
}

func (h *LocationShareController) listLocations(c fiber.Ctx) error {
	items, err := h.Shares.ListLocations(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	out := make([]friendLocationResponse, 0, len(items))
	for _, it := range items {
		out = append(out, friendLocationResponseFrom(it))
	}
	return c.JSON(httpx.Page[friendLocationResponse]{Items: out})
}

type areaResponse struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	RadiusM   int       `json:"radiusM"`
	CreatedAt time.Time `json:"createdAt"`
}

func areaResponseFrom(a friendmodel.Area) areaResponse {
	return areaResponse{ID: a.ID, Label: a.Label, Lat: a.Lat, Lon: a.Lon, RadiusM: a.RadiusM, CreatedAt: a.CreatedAt}
}

type areaCreateRequest struct {
	Label   *string  `json:"label"`
	Lat     *float64 `json:"lat"`
	Lon     *float64 `json:"lon"`
	RadiusM *int     `json:"radiusM"`
}

func (r *areaCreateRequest) Validate() map[string]string {
	f := map[string]string{}
	if r.Label == nil {
		f["label"] = "必須です"
	}
	if r.Lat == nil {
		f["lat"] = "必須です"
	}
	if r.Lon == nil {
		f["lon"] = "必須です"
	}
	if r.RadiusM == nil {
		f["radiusM"] = "必須です"
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

type areaUpdateRequest struct {
	Label   *string  `json:"label"`
	Lat     *float64 `json:"lat"`
	Lon     *float64 `json:"lon"`
	RadiusM *int     `json:"radiusM"`
}

func (h *LocationShareController) listAreas(c fiber.Ctx) error {
	areas, err := h.Shares.ListAreas(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	out := make([]areaResponse, 0, len(areas))
	for _, a := range areas {
		out = append(out, areaResponseFrom(a))
	}
	return c.JSON(httpx.Page[areaResponse]{Items: out})
}

func (h *LocationShareController) createArea(c fiber.Ctx) error {
	var req areaCreateRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	a, err := h.Shares.CreateArea(c.Context(), httpx.DID(c), friendsvc.AreaInput{
		Label: *req.Label, Lat: *req.Lat, Lon: *req.Lon, RadiusM: *req.RadiusM,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(areaResponseFrom(*a))
}

func (h *LocationShareController) updateArea(c fiber.Ctx) error {
	var req areaUpdateRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	a, err := h.Shares.UpdateArea(c.Context(), httpx.DID(c), c.Params("id"), friendsvc.AreaUpdate{
		Label: req.Label, Lat: req.Lat, Lon: req.Lon, RadiusM: req.RadiusM,
	})
	if err != nil {
		return err
	}
	return c.JSON(areaResponseFrom(*a))
}

func (h *LocationShareController) deleteArea(c fiber.Ctx) error {
	if err := h.Shares.DeleteArea(c.Context(), httpx.DID(c), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
