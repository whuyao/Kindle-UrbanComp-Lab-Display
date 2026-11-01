package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// The supplied laboratory logo is embedded unchanged; display conversion is
// performed locally, with no additional files or network requests on Kindle.
//
//go:embed assets/urbancomp-logo.png
var brandPNG []byte

func loadBrand() (image.Image, error) {
	source, err := png.Decode(bytes.NewReader(brandPNG))
	if err != nil {
		return nil, err
	}
	// Composite onto paper before increasing contrast. The shape and negative
	// space come directly from the source pixels, not a redrawn approximation.
	paper := image.NewRGBA(source.Bounds())
	draw.Draw(paper, paper.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(paper, paper.Bounds(), source, source.Bounds().Min, draw.Over)
	ink := image.NewGray(paper.Bounds())
	for y := ink.Bounds().Min.Y; y < ink.Bounds().Max.Y; y++ {
		for x := ink.Bounds().Min.X; x < ink.Bounds().Max.X; x++ {
			v := int(color.GrayModel.Convert(paper.At(x, y)).(color.Gray).Y)
			v = (v - 200) * 255 / 55
			if v < 0 {
				v = 0
			}
			ink.SetGray(x, y, color.Gray{Y: uint8(v)})
		}
	}
	return ink, nil
}

func (p *Painter) brandIcon(x, y, size int) error {
	r := image.Rect(x, y, x+size, y+size)
	if size <= 0 || !r.In(p.Img.Bounds()) {
		return fmt.Errorf("brand outside screen")
	}
	for _, prior := range p.Boxes {
		if !r.Intersect(prior).Empty() {
			return fmt.Errorf("brand overlaps content")
		}
	}
	p.Boxes = append(p.Boxes, r)
	xdraw.CatmullRom.Scale(p.Img, r, p.Logo, p.Logo.Bounds(), draw.Src, nil)
	return nil
}

func (p *Painter) brand(x, y, size, textSize int) error {
	if err := p.brandIcon(x, y, size); err != nil {
		return err
	}
	_, err := p.text("UrbanComp", x+size+9, y+(size-textSize)/2, textSize, "sans", 220, 1, true, 0)
	return err
}

func renderStartup(dir, path string) error {
	return withPainter(dir, path, func(p *Painter) error {
		if err := p.brandIcon(319, 280, 120); err != nil {
			return err
		}
		for _, line := range []struct {
			text    string
			y, size int
			face    string
		}{
			{"知新", 440, 64, "serif"}, {"UrbanComp", 533, 28, "sans"},
			{"正在读取科研内容", 599, 20, "sans"}, {"research.urbancomp.dev", 938, 18, "sans"},
		} {
			x := (758 - measure(p.face(line.face, line.size), line.text)) / 2
			if _, err := p.text(line.text, x, line.y, line.size, line.face, 678, 1, false, 0); err != nil {
				return err
			}
		}
		return nil
	})
}
