package controller

import (
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/utils"
)

// FeedPostController は公開投稿の作成と削除（17 §2.1・§2.2）。
type FeedPostController struct {
	Posts *service.PublicPostService
}

func (h *FeedPostController) Register(r fiber.Router) {
	r.Post("/feed/posts", h.create)
	r.Delete("/feed/posts/:rkey", h.delete)
}

type createFeedPostRequest struct {
	Text        string   `json:"text"`
	Langs       []string `json:"langs"`
	AlsoBluesky bool     `json:"alsoBluesky"`
}

func (r *createFeedPostRequest) Validate() map[string]string {
	f := map[string]string{}
	if r.Text == "" {
		f["text"] = "本文が必要です"
	} else if utils.GraphemeCount(r.Text) > service.MaxFeedPostGraphemes {
		f["text"] = "1000 文字までです"
	}
	if len(r.Langs) > service.MaxFeedPostLangs {
		f["langs"] = "3 件までです"
	}
	for i, l := range r.Langs {
		if !utils.ValidLanguageTag(l) {
			f["langs."+strconv.Itoa(i)] = "言語タグが不正です"
		}
	}
	if len(f) == 0 {
		return nil
	}
	return f
}

type bskyRefResponse struct {
	URI string `json:"uri"`
}

type feedPostResponse struct {
	URI  string           `json:"uri"`
	CID  string           `json:"cid"`
	Bsky *bskyRefResponse `json:"bsky,omitempty"`
}

func (h *FeedPostController) create(c fiber.Ctx) error {
	var req createFeedPostRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	res, err := h.Posts.Create(c.Context(), httpx.DID(c), httpx.SessionID(c), service.CreatePublicPostInput{
		Text:        req.Text,
		Langs:       req.Langs,
		AlsoBluesky: req.AlsoBluesky,
	})
	if err != nil {
		return err
	}
	out := feedPostResponse{URI: res.URI, CID: res.CID}
	if res.BskyURI != "" {
		out.Bsky = &bskyRefResponse{URI: res.BskyURI}
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

func (h *FeedPostController) delete(c fiber.Ctx) error {
	if err := h.Posts.Delete(c.Context(), httpx.DID(c), httpx.SessionID(c), c.Params("rkey")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
