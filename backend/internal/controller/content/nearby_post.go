package content

import (
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	contentsvc "github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/utils"
)

// NearbyPostController は nearby 投稿の作成（17 §2）。今近くにいる人だけに届ける限定投稿。
type NearbyPostController struct {
	Posts *contentsvc.NearbyPostService
}

func (h *NearbyPostController) Register(r fiber.Router) {
	r.Post("/nearby/posts", h.create)
}

type createNearbyPostRequest struct {
	Text string `json:"text"`
}

func (r *createNearbyPostRequest) Validate() map[string]string {
	f := map[string]string{}
	if r.Text == "" {
		f["text"] = "本文が必要です"
	} else if utils.GraphemeCount(r.Text) > contentsvc.MaxFeedPostGraphemes {
		f["text"] = "1000 文字までです"
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

type nearbyPostResponse struct {
	URI string `json:"uri"`
}

func (h *NearbyPostController) create(c fiber.Ctx) error {
	var req createNearbyPostRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	res, err := h.Posts.Create(c.Context(), httpx.DID(c), req.Text)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(nearbyPostResponse{URI: res.URI})
}
