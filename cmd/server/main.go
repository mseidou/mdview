package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"

	"github.com/yuzutech/kroki-go"
)

//
// コードブロック以外（普通のテキスト）を変更した場合でも毎度POSTするのは何かとお行儀が良くないので
// メモリ上にキャッシュを保持しておいて、変更が入ったらPOSTして新しいものを取得する方式にする
//
var cache map[[32]byte]string

type Mdview struct {
	docRoot		string
}

//
// kroki-go を使って mermaid 部分のテキストを zlib deflate -> base64 する
//
func generateKrokiURL(code string) string {

	data, err := kroki.CreatePayload(code)
	if err != nil {
		return ""
	}

	if len(data) > kroki.MAX_URI_LENGTH {
		return fmt.Sprintf("Base64 後の文字列長が GET パラメータの許容範囲を超えた")
	}

	baseUrl := "https://kroki.io"
	return fmt.Sprintf("%s/%s/%s/%s", baseUrl, kroki.Mermaid, kroki.SVG, data)
}

//
// ブロックコード mermaid の内容を kroki を使って API に投げて画像を取得する
// kroki 内部の generate.go 内では code の長さによって GET か POST かを判別してたけど、
// POST だと処理が重くなるとかいうことは無いだろうから全部POSTにしちゃう
//
// 取得したデータは dataURL として表示するために base64 で返す
//
// エラー時は空文字列を返す
// 何かエラーを返したとしてもHTML表示側としては何もすることはないので
//
// サーバ側のログとしてメッセージを出して置く
//
func getRenderedImage(code string) string {

	// cache にあればそれを返却
	sum := sha256.Sum256([]byte(code))
	if v, ok := cache[sum]; ok {
		return v
	}

	// なければ取得
	client := kroki.New(kroki.Configuration {
		URL:		"https://kroki.io",
		Timeout:	time.Second * 10,
	})

	image, err := client.FromString(code, kroki.Mermaid, kroki.SVG)
	if err != nil {
		fmt.Printf("kroki.PostRequest() failed: %v", err)
	}

	// base64 encode
	b64Image := base64.StdEncoding.EncodeToString([]byte(image))
//	fmt.Printf("Returned string: %s\n", b64Image)

	// cache に格納
	cache[sum] = b64Image

	return b64Image
}

//
// markdown 中に
//
// ```mermaid
// ```
//
// というブロックで mermaid のコードを書けるようにするために mermaid ようレンダラーを登録する
//
func mermaidRenderHook(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
	// コードブロックを見つけたら
	if code, ok := node.(*ast.CodeBlock); ok {
		// 言語が "mermaid" だったら
		if string(code.Info) == "mermaid" {
			// mermaid なら、 Kroki API に投げる
			mermaidCode := string(code.Literal)
			image := getRenderedImage(mermaidCode)
			fmt.Fprintf(w, `<img class="mermaid-image" src="data:image/svg+xml;base64,%s" />`, image)
//			url := generateKrokiURL(mermaidCode)
//			fmt.Fprintf(w, `<img class="mermaid-image" src="%s" />`, url)
			//fmt.Fprintf(w, `<img src="%s" />`, url)

			// 自分で処理したと伝える
			return ast.GoToNext, true
		}
	}
	// 自分の管轄じゃないッス
	return ast.GoToNext, false
}

func (m *Mdview) markdownHandler(w http.ResponseWriter, r *http.Request) {

	path := m.docRoot + r.URL.Path
	fmt.Printf("Requested: %s\n", path)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// mermaid 用レンダラー用意
	opts := html.RendererOptions {
		RenderNodeHook: mermaidRenderHook,
	}
	renderer := html.NewRenderer(opts)

	// 追加したいレンダラーは第三引数で渡す
	html := markdown.ToHTML(data, nil, renderer)
//	fmt.Printf("%s", html)

	fullHTML := fmt.Sprintf(`
<html>
<head>
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/github-markdown-css/5.2.0/github-markdown-light.min.css">
<style>
body { padding: 2em; }
//.markdown-body { max-width: 1200px; margin: auto; }
.markdown-body pre {
	white-space: pre-wrap;
	word-wrap: break-word;
}
.markdown-body { margin: auto; }
.markdown-body code {
	white-space: pre-wrap;
}

//.mermaid-image { width: 100%%; height: auto; }
.mermaid-image { width: auto; height: auto; }
</style>
</head>
<body>
<article class="markdown-body">%s</article>
</body>
</html>`, html)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(fullHTML))
}

func (m *Mdview) routeHandler(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, ".md") {
		m.markdownHandler(w, r)
		return
	}
	// .md 以外は FileServer()

	// FileServer() 辺りが 304 を返してしまい、ブラウザ側がキャッシュを見てしまうため
	// docRoot を変更してもブラウザ上は変わらないという現象になってしまっていたので対応
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.FileServer(http.Dir(m.docRoot)).ServeHTTP(w, r)
}

func main() {
	// キャッシュ初期化
	cache = make(map[[32]byte]string)
	mdview := &Mdview{}

	port := os.Getenv("MDVIEW_PORT")
	if port == "" {
		port = "18080"
	}

	mdview.docRoot = os.Getenv("MDVIEW_DOC_ROOT")
	if mdview.docRoot == "" {
		mdview.docRoot = "."
	}
	fmt.Printf("docRoot: %s\n", mdview.docRoot)

	mux := http.NewServeMux()

	// どの階層の *.md でも反応させるには、一旦 routeHandler に投げる
	mux.HandleFunc("/", mdview.routeHandler)

	log.Printf("Serving on port %s", port)
	log.Fatal(http.ListenAndServe(":" + port, mux))
}

