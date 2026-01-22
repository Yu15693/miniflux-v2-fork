// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mediaproxy // import "miniflux.app/v2/internal/mediaproxy"

import (
	"slices"
	"strings"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/reader/sanitizer"
	"miniflux.app/v2/internal/urllib"

	"github.com/PuerkitoBio/goquery"
	"github.com/gorilla/mux"
)

type urlProxyRewriter func(router *mux.Router, url string) string

// RewriteDocumentWithRelativeProxyURL 会扫描一段 HTML（正文片段），把其中的媒体资源 URL（img/audio/video 等）
// 重写成“相对路径”的代理 URL。
//
// 为什么要重写：
// - 让浏览器不直接请求第三方媒体资源，而是经过本服务的 /proxy 路由转发
// - 统一处理 mixed content（HTTP 资源在 HTTPS 页面里会被浏览器拦截）、隐私泄露、Referer 等问题
// - 支持按配置选择代理策略（全部/all、仅非 HTTPS、或完全关闭）
//
// 注意：
// - 这里处理的是 HTML 片段，并非完整页面；最终会尝试只返回 <body> 内部的 HTML。
func RewriteDocumentWithRelativeProxyURL(router *mux.Router, htmlDocument string) string {
	return genericProxyRewriter(router, ProxifyRelativeURL, htmlDocument)
}

// RewriteDocumentWithAbsoluteProxyURL 与 RewriteDocumentWithRelativeProxyURL 类似，
// 只是把媒体资源 URL 重写为“绝对 URL”形式的代理地址（包含 scheme/host）。
func RewriteDocumentWithAbsoluteProxyURL(router *mux.Router, htmlDocument string) string {
	return genericProxyRewriter(router, ProxifyAbsoluteURL, htmlDocument)
}

// genericProxyRewriter 是两种重写方式的公共实现：
// - proxifyFunction 决定生成的代理 URL 是相对还是绝对
// - htmlDocument 由 goquery 解析成 DOM 后，对指定标签/属性做 URL 改写
func genericProxyRewriter(router *mux.Router, proxifyFunction urlProxyRewriter, htmlDocument string) string {
	proxyOption := config.Opts.MediaProxyMode()
	if proxyOption == "none" {
		return htmlDocument
	}

	// 解析失败时选择“原样返回”，避免因解析器对非标准 HTML 不兼容而破坏内容展示。
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlDocument))
	if err != nil {
		return htmlDocument
	}

	// 根据配置决定要代理哪些资源类型（image/audio/video）。
	// 每种资源类型对应一组 HTML 标签与属性（img[src]、source[src]、video[poster] 等）。
	for _, mediaType := range config.Opts.MediaProxyResourceTypes() {
		switch mediaType {
		case "image":
			doc.Find("img, picture source").Each(func(i int, img *goquery.Selection) {
				if srcAttrValue, ok := img.Attr("src"); ok {
					// 将 img 设置为代理地址
					if shouldProxifyURL(srcAttrValue, proxyOption) {
						img.SetAttr("src", proxifyFunction(router, srcAttrValue))
					}
				}

				// srcset 需要逐个 candidate 改写，不能简单地对整个字符串做替换。
				if srcsetAttrValue, ok := img.Attr("srcset"); ok {
					proxifySourceSet(img, router, proxifyFunction, proxyOption, srcsetAttrValue)
				}
			})

			// 只有在“未启用 video 代理”时才处理 video poster：
			// - poster 是“视频封面图”，本质属于 image
			// - 若 video 已启用代理，会在 video case 里统一处理，避免重复扫描与重复改写
			if !slices.Contains(config.Opts.MediaProxyResourceTypes(), "video") {
				doc.Find("video").Each(func(i int, video *goquery.Selection) {
					if posterAttrValue, ok := video.Attr("poster"); ok {
						if shouldProxifyURL(posterAttrValue, proxyOption) {
							video.SetAttr("poster", proxifyFunction(router, posterAttrValue))
						}
					}
				})
			}

		case "audio":
			doc.Find("audio, audio source").Each(func(i int, audio *goquery.Selection) {
				if srcAttrValue, ok := audio.Attr("src"); ok {
					if shouldProxifyURL(srcAttrValue, proxyOption) {
						audio.SetAttr("src", proxifyFunction(router, srcAttrValue))
					}
				}
			})

		case "video":
			doc.Find("video, video source").Each(func(i int, video *goquery.Selection) {
				if srcAttrValue, ok := video.Attr("src"); ok {
					if shouldProxifyURL(srcAttrValue, proxyOption) {
						video.SetAttr("src", proxifyFunction(router, srcAttrValue))
					}
				}

				if posterAttrValue, ok := video.Attr("poster"); ok {
					if shouldProxifyURL(posterAttrValue, proxyOption) {
						video.SetAttr("poster", proxifyFunction(router, posterAttrValue))
					}
				}
			})
		}
	}

	// Miniflux 通常传入的是条目内容片段；这里只导出 <body> 内部 HTML，避免额外包裹 <html>/<head> 等标签。
	output, err := doc.FindMatcher(goquery.Single("body")).Html()
	if err != nil {
		return htmlDocument
	}

	return output
}

func proxifySourceSet(element *goquery.Selection, router *mux.Router, proxifyFunction urlProxyRewriter, proxyOption, srcsetAttrValue string) {
	// srcset 的语法允许多个候选资源（不同分辨率/宽度），需要先 parse 成结构化数据再逐一处理。
	imageCandidates := sanitizer.ParseSrcSetAttribute(srcsetAttrValue)

	for _, imageCandidate := range imageCandidates {
		if shouldProxifyURL(imageCandidate.ImageURL, proxyOption) {
			imageCandidate.ImageURL = proxifyFunction(router, imageCandidate.ImageURL)
		}
	}

	element.SetAttr("srcset", imageCandidates.String())
}

// shouldProxifyURL checks if the media URL should be proxified based on the media proxy option and URL scheme.
func shouldProxifyURL(mediaURL, mediaProxyOption string) bool {
	switch {
	case mediaURL == "":
		return false
	case strings.HasPrefix(mediaURL, "data:"):
		return false
	case mediaProxyOption == "all":
		return true
	case mediaProxyOption != "none" && !urllib.IsHTTPS(mediaURL):
		return true
	default:
		return false
	}
}

// ShouldProxifyURLWithMimeType checks if the media URL should be proxified based on the media proxy option, URL scheme, and MIME type.
func ShouldProxifyURLWithMimeType(mediaURL, mediaMimeType, mediaProxyOption string, mediaProxyResourceTypes []string) bool {
	if !shouldProxifyURL(mediaURL, mediaProxyOption) {
		return false
	}

	// 这里用 MIME 类型前缀匹配（image/*、audio/*、video/*），避免仅凭 URL 后缀做判断的不可靠性。
	for _, mediaType := range mediaProxyResourceTypes {
		if strings.HasPrefix(mediaMimeType, mediaType+"/") {
			return true
		}
	}

	return false
}
