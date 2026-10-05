package themes

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// MaxSVG is the largest SVG asset accepted, in bytes.
const MaxSVG = 256 << 10

// svgElements are the elements kept. Everything else is dropped with its
// children: script, foreignObject, image (external or data references),
// animate, animateTransform and set (they can rewrite href), style (it can
// import), a (links), iframe, and anything unknown.
var svgElements = map[string]bool{
	"svg": true, "g": true, "defs": true, "symbol": true, "use": true, "title": true, "desc": true,
	"path": true, "circle": true, "ellipse": true, "rect": true, "line": true, "polyline": true, "polygon": true,
	"text": true, "tspan": true,
	"linearGradient": true, "radialGradient": true, "stop": true, "pattern": true, "clipPath": true, "mask": true,
	"filter": true, "feGaussianBlur": true, "feOffset": true, "feMerge": true, "feMergeNode": true, "feBlend": true,
	"feColorMatrix": true, "feComposite": true, "feFlood": true, "feTurbulence": true, "feDisplacementMap": true,
	"feMorphology": true, "feComponentTransfer": true, "feFuncA": true, "feFuncR": true, "feFuncG": true, "feFuncB": true,
	"feDropShadow": true,
}

// svgAttrs are the attributes kept (case-sensitive SVG names).
var svgAttrs = func() map[string]bool {
	m := map[string]bool{}
	for _, a := range strings.Fields(`id class viewBox width height x y x1 y1 x2 y2 cx cy r rx ry fx fy fr
		d points transform fill fill-opacity fill-rule stroke stroke-width stroke-opacity stroke-linecap
		stroke-linejoin stroke-dasharray stroke-dashoffset stroke-miterlimit opacity
		offset stop-color stop-opacity gradientUnits gradientTransform spreadMethod patternUnits
		patternContentUnits patternTransform clipPathUnits maskUnits maskContentUnits clip-path clip-rule mask
		filter filterUnits primitiveUnits in in2 result stdDeviation dx dy mode operator k1 k2 k3 k4
		values type baseFrequency numOctaves seed stitchTiles scale xChannelSelector yChannelSelector
		radius flood-color flood-opacity tableValues slope intercept amplitude exponent
		font-family font-size font-weight font-style letter-spacing text-anchor dominant-baseline
		preserveAspectRatio href style role aria-label aria-hidden visibility display mix-blend-mode
		paint-order vector-effect shape-rendering color-interpolation-filters pathLength`) {
		m[a] = true
	}
	return m
}()

var (
	urlRE     = regexp.MustCompile(`(?i)url\s*\(\s*['"]?\s*([^)'"]*)`)
	badCSSRE  = regexp.MustCompile(`(?i)(expression|@import|javascript:|behavior|-moz-binding|</)`)
	jsSchemes = regexp.MustCompile(`(?i)^\s*(javascript|vbscript|data):`)
)

// safeValue reports whether an attribute value is safe: every url() points
// at a fragment in the same document, and nothing script-like appears.
func safeValue(v string) bool {
	if badCSSRE.MatchString(v) || jsSchemes.MatchString(v) {
		return false
	}
	for _, m := range urlRE.FindAllStringSubmatch(v, -1) {
		if !strings.HasPrefix(strings.TrimSpace(m[1]), "#") {
			return false
		}
	}
	return true
}

// SanitizeSVG parses an SVG document and re-serialises only the allowed
// elements and attributes. Comments, processing instructions and DOCTYPEs
// (which could declare entities) are dropped; event handlers, external
// references and script-carrying elements are removed. The result is a
// standalone SVG that is safe to serve as an image.
func SanitizeSVG(in []byte) ([]byte, error) {
	if len(in) > MaxSVG {
		return nil, fmt.Errorf("svg is larger than %d KB", MaxSVG>>10)
	}
	dec := xml.NewDecoder(bytes.NewReader(in))
	dec.Strict = true
	dec.Entity = nil // only the five predefined XML entities
	var out bytes.Buffer
	skip := 0 // depth inside a dropped element
	depth := 0
	sawRoot := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("svg is not well-formed XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if skip > 0 {
				skip++
				continue
			}
			name := t.Name.Local
			if depth == 0 {
				if sawRoot || name != "svg" {
					return nil, errors.New("svg: the root element must be <svg>")
				}
				sawRoot = true
			}
			if !svgElements[name] || (t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg") {
				skip = 1
				continue
			}
			depth++
			out.WriteString("<" + name)
			if depth == 1 {
				out.WriteString(` xmlns="http://www.w3.org/2000/svg"`)
			}
			for _, a := range t.Attr {
				an := a.Name.Local
				// xlink:href and href: same-document fragments only.
				if an == "href" {
					if !strings.HasPrefix(strings.TrimSpace(a.Value), "#") {
						continue
					}
				} else if a.Name.Space != "" || !svgAttrs[an] || strings.HasPrefix(strings.ToLower(an), "on") {
					continue
				}
				if !safeValue(a.Value) {
					continue
				}
				out.WriteString(" " + an + `="`)
				_ = xml.EscapeText(&out, []byte(a.Value))
				out.WriteString(`"`)
			}
			out.WriteString(">")
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			depth--
			out.WriteString("</" + t.Name.Local + ">")
		case xml.CharData:
			if skip == 0 && depth > 0 {
				_ = xml.EscapeText(&out, t)
			}
		}
		// Comments, ProcInst and Directive are dropped.
	}
	if !sawRoot {
		return nil, errors.New("svg: no <svg> element")
	}
	return out.Bytes(), nil
}
