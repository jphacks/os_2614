package controller

import (
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/service"
)

type InstanceController struct {
	Instances *service.InstanceService
}

func (h *InstanceController) Register(r fiber.Router) {
	r.Get("/instances", h.listInstances)
	r.Post("/posts", h.createPost)
	r.Get("/instances/:spaceId/posts", h.listPosts)
	r.Delete("/instances/:spaceId/posts/:rkey", h.deletePost)
}

type mediaRefRequest struct {
	ID  string `json:"id"`
	Alt string `json:"alt"`
}

type createPostRequest struct {
	SpaceIDs   []string          `json:"spaceIds"`
	Text       string            `json:"text"`
	Format     string            `json:"format"`
	Media      []mediaRefRequest `json:"media"`
	AlsoPublic bool              `json:"alsoPublic"`
}

func (r *createPostRequest) Validate() map[string]string {
	f := map[string]string{}
	if len(r.SpaceIDs) < 1 || len(r.SpaceIDs) > 5 {
		f["spaceIds"] = "1〜5 件で指定してください"
	}
	if len(r.Media) > 4 {
		f["media"] = "4 件までです"
	}
	if len(r.Media) > 0 && len(r.SpaceIDs) != 1 {
		f["media"] = "メディアを付けられるのは 1 つの Space への投稿だけです"
	}
	if r.Format != "" && r.Format != "plain" && r.Format != "markdown" {
		f["format"] = "plain / markdown のいずれかです"
	}
	if r.Text == "" && len(r.Media) == 0 {
		f["text"] = "本文かメディアが必要です"
	}
	if utf8.RuneCountInString(r.Text) > 2000 {
		f["text"] = "2000 文字までです"
	}
	for i, m := range r.Media {
		if m.ID == "" {
			f["media."+strconv.Itoa(i)+".id"] = "id が必要です"
		}
	}
	return f
}

func (h *InstanceController) createPost(c fiber.Ctx) error {
	var req createPostRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	if req.AlsoPublic {
		return httpx.ErrInvalidRequest.With("fields", map[string]string{"alsoPublic": "未対応です"})
	}
	format := req.Format
	if format == "" {
		format = "plain"
	}
	media := make([]service.MediaInput, 0, len(req.Media))
	for _, m := range req.Media {
		media = append(media, service.MediaInput{ID: m.ID, Alt: m.Alt})
	}
	res, err := h.Instances.CreatePosts(c.Context(), httpx.DID(c), service.CreatePostInput{
		SpaceIDs: req.SpaceIDs,
		Text:     req.Text,
		Format:   format,
		Media:    media,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(res)
}

func (h *InstanceController) listPosts(c fiber.Ctx) error {
	page, err := h.Instances.ListPosts(c.Context(), httpx.DID(c), c.Params("spaceId"), c.Query("cursor"), pageLimit(c))
	if err != nil {
		return err
	}
	return c.JSON(page)
}

func (h *InstanceController) deletePost(c fiber.Ctx) error {
	if err := h.Instances.DeletePost(c.Context(), httpx.DID(c), c.Params("spaceId"), c.Params("rkey")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type instanceItem struct {
	SpaceID     string     `json:"spaceId"`
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	State       string     `json:"state"`
	MemberCount int        `json:"memberCount"`
	LastPostAt  *time.Time `json:"lastPostAt,omitempty"`
	ArchivedAt  *time.Time `json:"archivedAt,omitempty"`
}

type instancesResponse struct {
	Items []instanceItem `json:"items"`
}

func (h *InstanceController) listInstances(c fiber.Ctx) error {
	items, err := h.Instances.ListInstances(c.Context(), httpx.DID(c))
	if err != nil {
		return err
	}
	resp := instancesResponse{Items: make([]instanceItem, 0, len(items))}
	for _, it := range items {
		resp.Items = append(resp.Items, instanceItem{
			SpaceID:     it.Space.ID,
			Type:        spaceTypeName(it.Space.Type),
			Name:        it.Space.Name,
			State:       it.Space.State,
			MemberCount: it.MemberCount,
			LastPostAt:  it.LastPostAt,
			ArchivedAt:  it.Space.ArchivedAt,
		})
	}
	return c.JSON(resp)
}

func spaceTypeName(nsid string) string {
	switch nsid {
	case "jp.i32.chain.space.group":
		return "group"
	case "jp.i32.chain.space.party":
		return "party"
	default:
		return nsid
	}
}

// pageLimit は 02 §4 の limit（既定 30、最大 100）。
func pageLimit(c fiber.Ctx) int {
	n, err := strconv.Atoi(c.Query("limit"))
	if err != nil || n <= 0 {
		return 30
	}
	if n > 100 {
		return 100
	}
	return n
}
