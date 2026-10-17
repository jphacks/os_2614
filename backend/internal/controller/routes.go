package controller

import (
	"github.com/gofiber/fiber/v3"
)

// Registrar はルートを登録できる controller。
type Registrar interface {
	Register(r fiber.Router)
}

// Routes は main.go から渡される、ルートの割り当て。
type Routes struct {
	// Root は /health・OAuth メタデータ・/auth・/media など、/api/v1 の外に置くもの。
	Root []Registrar
	// Public は /api/v1 配下で認証不要のもの（/auth/exchange、/auth/refresh）。
	Public []Registrar
	// Private は /api/v1 配下で認証が要るもの。
	Private []Registrar
}

func Mount(app *fiber.App, requireAuth fiber.Handler, routes Routes) {
	for _, c := range routes.Root {
		c.Register(app)
	}
	api := app.Group("/api/v1")
	for _, c := range routes.Public {
		c.Register(api)
	}
	private := api.Group("", requireAuth)
	for _, c := range routes.Private {
		c.Register(private)
	}
}
