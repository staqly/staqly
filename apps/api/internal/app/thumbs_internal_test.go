package app

import (
	"image"
	"image/color"
	"testing"
)

// A small PNG can declare a 16-bit 49 MP picture (a 392 KB file decodes to 373 MB): decodes are bounded by the
// memory they take, not only by CPU count, so a few such uploads can't run the server out of memory at once.
func TestThumbDecodesShareAMemoryBudget(t *testing.T) {
	big := image.Config{ColorModel: color.NRGBA64Model, Width: 7000, Height: 7000}
	if got := decodeCost(big); got != 7000*7000*8 {
		t.Fatalf("cost of a 16-bit picture: %d", got)
	}
	if got := decodeCost(image.Config{ColorModel: color.GrayModel, Width: 100, Height: 100}); got != 100*100 {
		t.Fatalf("cost of a gray picture: %d", got)
	}
	th := newThumbnailer()
	if !th.mem.TryAcquire(decodeCost(big)) {
		t.Fatal("one large decode must fit the budget")
	}
	if th.mem.TryAcquire(decodeCost(big)) {
		t.Fatal("a second large decode must wait for the first")
	}
	th.mem.Release(decodeCost(big))
	if !th.mem.TryAcquire(decodeCost(big)) {
		t.Fatal("released budget is available again")
	}
}
