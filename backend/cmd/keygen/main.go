// keygen は OAuth confidential client 用の P-256 秘密鍵を multibase で出力する（CHAIN_OAUTH_PRIVATE_KEY）。
package main

import (
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
)

func main() {
	k, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		fmt.Fprintln(os.Stderr, "keygen:", err)
		os.Exit(1)
	}
	fmt.Println(k.Multibase())
}
