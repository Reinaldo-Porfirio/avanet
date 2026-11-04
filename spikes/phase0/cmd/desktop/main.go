//go:build windows

// Spike (f): tray icon and toast notification from a normal (non-admin) user.
// Shows the icon for 20 seconds, sends one notification, then exits.
//
// The toast library registers the app under
// HKCU\SOFTWARE\Classes\AppUserModelId\<AppID>; remove it after the test with:
//
//	reg delete "HKCU\SOFTWARE\Classes\AppUserModelId\AvaNet.Spike" /f
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"time"

	"fyne.io/systray"
	"git.sr.ht/~jackmordaunt/go-toast/v2"
)

const appID = "AvaNet.Spike"

func main() {
	systray.Run(onReady, func() { fmt.Println("bandeja: encerrada") })
}

func onReady() {
	icon, err := makeIcon()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ícone:", err)
		systray.Quit()
		return
	}
	systray.SetIcon(icon)
	systray.SetTooltip("AvaNet (teste da Fase 0)")
	quit := systray.AddMenuItem("Sair", "Encerrar o teste")
	fmt.Println("bandeja: ícone exibido")

	if err := toast.SetAppData(toast.AppData{AppID: appID}); err != nil {
		fmt.Println("notificação: ERRO ao registrar app:", err)
	}
	n := toast.Notification{
		AppID: appID,
		Title: "Seu boletim de ontem está pronto",
		Body:  "Dia firme, noite instável. Toque para ver o relatório. (teste da Fase 0)",
	}
	if err := n.Push(); err != nil {
		fmt.Println("notificação: ERRO:", err)
	} else {
		fmt.Println("notificação: enviada")
	}

	go func() {
		select {
		case <-quit.ClickedCh:
		case <-time.After(20 * time.Second):
		}
		systray.Quit()
	}()
}

// makeIcon draws a 32x32 PNG and wraps it in an ICO container, which is the
// format the Windows tray expects (PNG-in-ICO is supported since Vista).
func makeIcon() ([]byte, error) {
	const size = 32
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	ink := color.NRGBA{R: 0x2e, G: 0x7a, B: 0x6b, A: 0xff}
	for y := range size {
		for x := range size {
			dx, dy := x-size/2, y-size/2
			if dx*dx+dy*dy <= 14*14 {
				img.Set(x, y, ink)
			}
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, err
	}

	var ico bytes.Buffer
	header := []any{
		uint16(0), uint16(1), uint16(1), // reserved, type=icon, count=1
		uint8(size), uint8(size), uint8(0), uint8(0), // width, height, colors, reserved
		uint16(1), uint16(32), // planes, bits per pixel
		uint32(pngBuf.Len()), uint32(6 + 16), // data size, data offset
	}
	for _, v := range header {
		if err := binary.Write(&ico, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	ico.Write(pngBuf.Bytes())
	return ico.Bytes(), nil
}
