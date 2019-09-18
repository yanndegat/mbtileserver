package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/consbio/mbtileserver/mbtiles"
	sm "github.com/flopp/go-staticmaps"
	"github.com/golang/geo/s2"
)

const (
	DEFAULT_STATICMAP_WIDTH  = 600
	DEFAULT_STATICMAP_HEIGHT = 400
)

// TileFetcher downloads map tile images from a TileProvider
type LocalDBTileFetcher struct {
	db *mbtiles.DB
}

// sm.TileFetcher.SetUserAgent
func (t *LocalDBTileFetcher) SetUserAgent(a string) {
	// not implemented, useless in our case
	return
}

// sm.TileFetcher.SetUserAgent
func (t LocalDBTileFetcher) Fetch(z, x, y int) (image.Image, error) {
	var data []byte
	// flip y to match the spec
	y = (1 << uint64(z)) - 1 - y
	err := t.db.ReadTile(uint8(z), uint64(x), uint64(y), &data)

	if err != nil {
		// augment error info
		err = fmt.Errorf("cannot fetch tile from DB for z=%d, x=%d, y=%d: %v", z, x, y, err)
		return nil, err
	}

	if data == nil || len(data) <= 1 {
		err = fmt.Errorf("Tile not found from DB for z=%d, x=%d, y=%d: %v", z, x, y, err)
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	return img, nil
}

func handleBackgroundOption(ctx *sm.Context, p string) error {
	if p == "" {
		return nil
	}

	color, err := sm.ParseColorString(p)
	if err != nil {
		return err
	}

	ctx.SetBackground(color)
	return nil
}

func handlePathsOption(ctx *sm.Context, p string) error {
	if p == "" {
		return nil
	}

	paths, err := sm.ParsePathString(p)
	if err != nil {
		return err
	} else {
		for _, path := range paths {
			ctx.AddPath(path)
		}
	}
	return nil
}

func (s *ServiceSet) staticmap(db *mbtiles.DB) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) (int, error) {
		ctx := sm.NewContext()
		fetcher := &LocalDBTileFetcher{
			db: db,
		}
		ctx.SetTileFetcher(fetcher)

		// split path components to extract tile coordinates x, y and z
		pcs := strings.Split(r.URL.Path[1:], "/")
		// we are expecting at least "services", <id> , "staticmap", <z>, <x>, <y plus .ext>
		l := len(pcs)
		if l < 6 || pcs[5] == "" {
			return http.StatusBadRequest, fmt.Errorf("requested path is too short")
		}

		query := r.URL.Query()

		rawWidth := query.Get("width")
		width := DEFAULT_STATICMAP_WIDTH
		if rawWidth == "" {
			log.Printf("staticmap: use default width")
		} else if v, err := strconv.Atoi(rawWidth); err != nil {
			log.Printf("staticmap: cannot parse width %q: %v", rawWidth, err)
		} else {
			width = v
		}

		rawHeight := query.Get("height")
		height := DEFAULT_STATICMAP_HEIGHT
		if rawHeight == "" {
			log.Printf("staticmap: use default height")
		} else if v, err := strconv.Atoi(rawHeight); err != nil {
			log.Printf("staticmap: cannot parse height %q: %v", rawHeight, err)
		} else {
			height = v
		}
		ctx.SetSize(width, height)

		if v, err := strconv.Atoi(pcs[l-3]); err != nil {
			return http.StatusBadRequest, fmt.Errorf("staticmap: cannot parse zoom %q: %v", pcs[l-3], err)
		} else {
			ctx.SetZoom(v)
		}

		var x, y float64
		if v, err := strconv.ParseFloat(pcs[l-2], 64); err != nil {
			log.Printf("staticmap: cannot parse x %q: %v", pcs[l-2], err)
		} else {
			x = v
		}

		if v, err := strconv.ParseFloat(pcs[l-1], 64); err != nil {
			log.Printf("staticmap: cannot parse y %q: %v", pcs[l-1], err)
		} else {
			y = v
		}

		ctx.SetCenter(s2.LatLngFromDegrees(x, y))

		background := query.Get("background")
		if err := handleBackgroundOption(ctx, background); err != nil {
			return http.StatusBadRequest, fmt.Errorf("staticmap: cannot parse background %q: %v", background, err)
		}

		path := query.Get("path")
		if err := handlePathsOption(ctx, path); err != nil {
			return http.StatusBadRequest, fmt.Errorf("staticmap: cannot parse path %q: %v", path, err)
		}

		img, err := ctx.Render()
		if err != nil {
			// augment error info
			err = fmt.Errorf("cannot fetch tile from DB for z=%d, x=%d, y=%d: %v", pcs[l-3], pcs[l-2], pcs[l-1], err)
			return http.StatusInternalServerError, err
		}

		if img == nil {
			return tileNotFoundHandler(w, db.TileFormat())
		}

		w.Header().Set("Content-Type", db.ContentType())
		if db.TileFormat() == mbtiles.PBF {
			w.Header().Set("Content-Encoding", "gzip")
		}
		err = png.Encode(w, img)
		if err != nil {
			err = fmt.Errorf("cannot write image: %v", err)
			return http.StatusInternalServerError, err
		}
		return http.StatusOK, err

	}
}
