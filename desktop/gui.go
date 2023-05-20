package desktop

import (
	"image"
	"image/draw"
	"math"
	"time"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/BurntSushi/freetype-go/freetype/truetype"
	"github.com/BurntSushi/xgbutil/ewmh"
	"github.com/BurntSushi/xgbutil/icccm"
	"github.com/BurntSushi/xgbutil/motif"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xgraphics"
	"github.com/BurntSushi/xgbutil/xwindow"

	"github.com/leukipp/cortile/common"
	"github.com/leukipp/cortile/store"

	log "github.com/sirupsen/logrus"
)

var (
	gui *xwindow.Window // Layout overlay window
)

var (
	fontSize   int = 16 // Size of text font
	fontMargin int = 4  // Margin of text font
	rectMargin int = 4  // Margin of layout rectangles
)

func ShowLayout(ws *Workspace) {
	if common.Config.TilingGui <= 0 {
		return
	}

	// Obtain layout infos
	al := ws.ActiveLayout()
	mg := al.GetManager()
	name := al.GetName()

	// Calculate scaled desktop dimensions
	dx, dy, dw, dh := common.DesktopDimensions()
	_, _, width, height := scale(dx, dy, dw, dh)

	// Create an empty canvas image
	bg := bgra("gui_background")
	cv := xgraphics.New(common.X, image.Rect(0, 0, width+rectMargin, height+fontSize+2*fontMargin+rectMargin))
	cv.For(func(x int, y int) xgraphics.BGRA { return bg })

	// Wait for tiling events
	time.AfterFunc(100*time.Millisecond, func() {

		// Draw client rectangles
		drawClients(cv, mg, name)

		// Draw layout name
		drawText(cv, name, bgra("gui_text"), cv.Rect.Dx()/2, cv.Rect.Dy()-fontSize-2*fontMargin)

		// Show the canvas graphics
		showGraphics(cv, time.Duration(common.Config.TilingGui))
	})
}

func drawClients(cv *xgraphics.Image, mg *store.Manager, layout string) {
	clients := mg.Clients(false)
	for _, c := range clients {
		for _, state := range c.Latest.States {
			if state == "_NET_WM_STATE_FULLSCREEN" || layout == "fullscreen" {
				clients = mg.Visible(&store.Windows{Clients: mg.Clients(true), Allowed: 1})
				break
			}
		}
	}

	// Draw default rectangle
	if len(clients) == 0 {

		// Calculate scaled desktop dimensions
		_, _, dw, dh := common.DesktopDimensions()
		x, y, width, height := scale(0, 0, dw, dh)

		// Draw client rectangle onto canvas
		color := bgra("gui_client_slave")
		rect := &image.Uniform{color}
		drawImage(cv, rect, color, x+rectMargin, y+rectMargin, x+width, y+height)

		return
	}

	// Draw master and slave rectangle
	for _, c := range clients {

		// Calculate scaled client dimensions
		cx, cy, cw, ch := c.OuterGeometry()
		dx, dy, _, _ := common.DesktopDimensions()
		x, y, width, height := scale(cx-dx, cy-dy, cw, ch)

		// Calculate icon size
		iconSize := math.MaxInt
		if width < iconSize {
			iconSize = width
		}
		if height < iconSize {
			iconSize = height
		}
		iconSize /= 2

		// Obtain rectangle color
		color := bgra("gui_client_slave")
		if mg.IsMaster(c) || layout == "fullscreen" {
			color = bgra("gui_client_master")
		}

		// Draw client rectangle onto canvas
		rect := &image.Uniform{color}
		drawImage(cv, rect, color, x+rectMargin, y+rectMargin, x+width, y+height)

		// Draw client icon onto canvas
		ico, err := xgraphics.FindIcon(common.X, c.Win.Id, iconSize, iconSize)
		if err == nil {
			drawImage(cv, ico, color, x+rectMargin/2+width/2-iconSize/2, y+rectMargin/2+height/2-iconSize/2, x+width, y+height)
		}
	}
}

func drawImage(cv *xgraphics.Image, img image.Image, color xgraphics.BGRA, x0 int, y0 int, x1 int, y1 int) {
	draw.Draw(cv, image.Rect(x0, y0, x1, y1), img, image.Point{}, draw.Src)
	xgraphics.BlendBgColor(cv, color)
}

func drawText(cv *xgraphics.Image, txt string, color xgraphics.BGRA, x int, y int) {
	font, err := truetype.Parse(goregular.TTF)
	if err != nil {
		log.Error(err)
		return
	}

	// Draw text onto canvas
	w, _ := xgraphics.Extents(font, float64(fontSize), txt)
	cv.Text(x-w/2, y, color, float64(fontSize), font, txt)
}

func showGraphics(img *xgraphics.Image, duration time.Duration) *xwindow.Window {
	win, err := xwindow.Generate(img.X)
	if err != nil {
		log.Error(err)
		return nil
	}

	// Create a window with dimensions equal to the image
	width, height := img.Rect.Dx(), img.Rect.Dy()
	win.Create(img.X.RootWin(), 0, 0, width, height, 0)
	ewmh.WmNameSet(win.X, win.Id, "cortile")

	// Set states for modal like behavior
	icccm.WmStateSet(win.X, win.Id, &icccm.WmState{
		State: icccm.StateNormal,
	})
	ewmh.WmStateSet(win.X, win.Id, []string{
		"_NET_WM_STATE_SKIP_TASKBAR",
		"_NET_WM_STATE_SKIP_PAGER",
		"_NET_WM_STATE_ABOVE",
	})

	// Set hints for size and decorations
	icccm.WmNormalHintsSet(img.X, win.Id, &icccm.NormalHints{
		Flags:     icccm.SizeHintPMinSize | icccm.SizeHintPMaxSize,
		MinWidth:  uint(width),
		MinHeight: uint(height),
		MaxWidth:  uint(width),
		MaxHeight: uint(height),
	})
	motif.WmHintsSet(img.X, win.Id, &motif.Hints{
		Flags:      motif.HintFunctions | motif.HintDecorations,
		Function:   motif.FunctionNone,
		Decoration: motif.DecorationNone,
	})

	// Ensure the window closes gracefully
	win.WMGracefulClose(func(w *xwindow.Window) {
		xevent.Detach(w.X, w.Id)
		xevent.Quit(w.X)
		w.Destroy()
	})

	// Paint the image and map the window
	img.XSurfaceSet(win.Id)
	img.XPaint(win.Id)
	img.XDraw()
	win.Map()

	// Close previous opened window
	if gui != nil {
		gui.Destroy()
	}
	gui = win

	// Close window after given duration
	if duration > 0 {
		time.AfterFunc(duration*time.Millisecond, win.Destroy)
	}

	return win
}

func bgra(name string) xgraphics.BGRA {
	rgba := common.Config.Colors[name]

	// Validate length
	if len(rgba) != 4 {
		log.Warn("Error obtaining color for ", name)
		return xgraphics.BGRA{}
	}

	return xgraphics.BGRA{
		R: uint8(rgba[0]),
		G: uint8(rgba[1]),
		B: uint8(rgba[2]),
		A: uint8(rgba[3]),
	}
}

func scale(x, y, w, h int) (sx, sy, sw, sh int) {
	s := 10
	sx, sy, sw, sh = x/s, y/s, w/s, h/s
	return
}
