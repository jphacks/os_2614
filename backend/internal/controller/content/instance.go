package content

import (
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/controller"
	"github.com/applemango/chain/internal/httpx"
	contentsvc "github.com/applemango/chain/internal/service/content"
)

type InstanceController struct {
	Instances *contentsvc.InstanceService
	Pins      *contentsvc.PinService
	Feed      *contentsvc.FeedService
}

func (h *InstanceController) Register(r fiber.Router) {
	r.Get("/instances", h.listInstances)
	r.Post("/posts", h.createPost)
	r.Get("/instances/:spaceId/posts", h.listPosts)
	r.Delete("/instances/:spaceId/posts/:rkey", h.deletePost)
	r.Get("/instances/:spaceId/pins", h.listPins)
	r.Post("/instances/:spaceId/pins", h.createPin)
	r.Delete("/instances/:spaceId/pins/:rkey", h.deletePin)
}

type mediaRefRequest struct {
	ID  string `json:"id"`
	Alt string `json:"alt"`
}

type replyRequest struct {
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type createPostRequest struct {
	SpaceIDs    []string          `json:"spaceIds"`
	Text        string            `json:"text"`
	Format      string            `json:"format"`
	Media       []mediaRefRequest `json:"media"`
	Reply       *replyRequest     `json:"reply"`
	AlsoPublic  bool              `json:"alsoPublic"`
	SelfLabels  []string          `json:"selfLabels"`
	AlsoBluesky bool              `json:"alsoBluesky"`
}

func (r *createPostRequest) Validate() map[string]string {
	f := map[string]string{}
	if len(r.SpaceIDs) < 1 || len(r.SpaceIDs) > 5 {
		f["spaceIds"] = "1〜5 件で指定してください"
	}
	if _, ok := contentsvc.NormalizeSelfLabels(r.SelfLabels); !ok {
		f["selfLabels"] = "sexual / violence / self_harm / spoiler のどれかを指定してください"
	}
	if len(r.Media) > 4 {
		f["media"] = "4 件までです"
	}
	if len(r.Media) > 0 && len(r.SpaceIDs) != 1 {
		f["media"] = "メディアを付けられるのは 1 つの Space への投稿だけです"
	}
	if r.Reply != nil {
		if len(r.SpaceIDs) != 1 {
			f["reply"] = "返信できるのは 1 つの Space への投稿だけです"
		}
		if r.Reply.Root == "" || r.Reply.Parent == "" {
			f["reply"] = "root と parent が必要です"
		}
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
	format := req.Format
	if format == "" {
		format = "plain"
	}
	media := make([]contentsvc.MediaInput, 0, len(req.Media))
	for _, m := range req.Media {
		media = append(media, contentsvc.MediaInput{ID: m.ID, Alt: m.Alt})
	}
	var reply *contentsvc.ReplyInput
	if req.Reply != nil {
		reply = &contentsvc.ReplyInput{Root: req.Reply.Root, Parent: req.Reply.Parent}
	}
	res, err := h.Instances.CreatePosts(c.Context(), httpx.DID(c), contentsvc.CreatePostInput{
		SpaceIDs:    req.SpaceIDs,
		Text:        req.Text,
		Format:      format,
		Media:       media,
		Reply:       reply,
		SessionID:   httpx.SessionID(c),
		AlsoPublic:  req.AlsoPublic,
		SelfLabels:  req.SelfLabels,
		AlsoBluesky: req.AlsoBluesky,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(res)
}

func (h *InstanceController) listPosts(c fiber.Ctx) error {
	page, err := h.Feed.List(c.Context(), httpx.DID(c), "space:"+c.Params("spaceId"), c.Query("cursor"), controller.PageLimit(c))
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

type pinRequest struct {
	Subject string `json:"subject"`
}

func (r *pinRequest) Validate() map[string]string {
	if r.Subject == "" {
		return map[string]string{"subject": "必須です"}
	}
	return nil
}

func (h *InstanceController) createPin(c fiber.Ctx) error {
	var req pinRequest
	if err := httpx.Bind(c, &req); err != nil {
		return err
	}
	pin, err := h.Pins.CreateManual(c.Context(), httpx.DID(c), c.Params("spaceId"), req.Subject)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(pin)
}

func (h *InstanceController) deletePin(c fiber.Ctx) error {
	if err := h.Pins.Delete(c.Context(), httpx.DID(c), c.Params("spaceId"), c.Params("rkey")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type pinsResponse struct {
	Items []contentsvc.Pin `json:"items"`
}

func (h *InstanceController) listPins(c fiber.Ctx) error {
	pins, err := h.Pins.List(c.Context(), httpx.DID(c), c.Params("spaceId"))
	if err != nil {
		return err
	}
	return c.JSON(pinsResponse{Items: pins})
}

type instanceItem struct {
	SpaceID     string     `json:"spaceId"`
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	State       string     `json:"state"`
	MemberCount int        `json:"memberCount"`
	CanWrite    bool       `json:"canWrite"`
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
			CanWrite:    it.CanWrite,
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
