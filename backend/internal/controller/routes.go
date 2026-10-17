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

// Mount はルートを fiber.App に登録する。
// rateLimit は 2 箇所に差す。app 全体には /auth/* の IP 単位の制限のため（認証前は DID が無いので
// 他のルートは素通りする）、認証グループには DID 単位の制限のため。requireAuth の後に来るので DID は設定済み。
func Mount(app *fiber.App, requireAuth fiber.Handler, rateLimit fiber.Handler, routes Routes) {
	if rateLimit != nil {
		app.Use(rateLimit)
	}
	for _, c := range routes.Root {
		c.Register(app)
	}
	api := app.Group("/api/v1")
	for _, c := range routes.Public {
		c.Register(api)
	}
	privateHandlers := []any{requireAuth}
	if rateLimit != nil {
		privateHandlers = append(privateHandlers, rateLimit)
	}
	private := api.Group("", privateHandlers...)
	for _, c := range routes.Private {
		c.Register(private)
	}
}
