package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"

	"github.com/yuzutech/kroki-go"
)

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
			url := generateKrokiURL(mermaidCode)
			fmt.Fprintf(w, `<img class="mermaid-image" src="%s" />`, url)
			//fmt.Fprintf(w, `<img src="%s" />`, url)

			// 自分で処理したと伝える
			return ast.GoToNext, true
		}
	}
	// 自分の管轄じゃないッス
	return ast.GoToNext, false
}

func handler(w http.ResponseWriter, r *http.Request) {
	doc_root := os.Getenv("MD_DOC_ROOT")
	if doc_root == "" {
		doc_root = "."
//		fmt.Printf("Error: 環境変数 MD_DOC_ROOT が未定義")
//		os.Exit(1)
	}
	// この時点では doc_root の存在チェックはしない

	path := doc_root + r.URL.Path
	if path[len(path)-3:] == ".md" {
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
//		fmt.Printf("%s", html)

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

	} else {
		http.FileServer(http.Dir(".")).ServeHTTP(w, r)
	}
}

func main() {
	http.HandleFunc("/", handler)
	log.Println("Serving on :18080")
	log.Fatal(http.ListenAndServe(":18080", nil))
}

