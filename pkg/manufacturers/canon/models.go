package canon

import "fmt"

// VendorID is Canon Inc.'s USB vendor ID.
const VendorID uint16 = 0x04a9

// Counter is one value in MPU memory, named and located.
type Counter struct {
	// Label is what the CLI prints.
	Label string `json:"label"`

	// Address is the MPU memory address, as passed to MonReadAndGetData.
	Address uint32 `json:"mpu_address"`

	// Length is how many bytes to read. Every counter observed so far
	// answers in ten.
	Length uint32 `json:"length"`
}

// Model is what is known about one camera body.
type Model struct {
	// Name is the marketing name.
	Name string `json:"name"`

	// ProductID is the USB product ID the body enumerates with.
	ProductID uint16 `json:"product_id"`

	// RatedLife is the manufacturer's shutter durability rating, used only
	// to put the reading in context. Zero means unknown.
	RatedLife uint32 `json:"rated_life,omitempty"`

	// Counters is the MPU address map.
	Counters []Counter `json:"counters"`

	// Confirmed records whether somebody with this body checked the
	// readings against a known number of frames and reported back. An
	// unconfirmed map is a reasonable guess and the CLI says so.
	Confirmed bool `json:"confirmed"`

	// ConfirmedOn describes the firmware it was confirmed against.
	ConfirmedOn string `json:"confirmed_on,omitempty"`
}

// The factory channel is only sent to the bodies below, and to bodies that list
// 0x9052 and 0x9053 (GuessModel). An EOS R8 (seen 2026-11-04 over macOS) gave
// no reply to MonOpen, then stopped answering USB at all, even CloseSession,
// until it was switched off; it doesn't list 0x9052 or 0x9053. A 90D hung the
// same way in upstream issue #4, and libgphoto2's 90D dump doesn't list them
// either.
//
// Each address map below was established differentially: read every address,
// take an exact number of frames, read again, and keep whatever advanced by
// exactly that number. Adding a body is the same job, and docs/CONTRIBUTING.md
// walks through it.
//
// On the 5D Mark IV, MPU 0x1198 also answers, in 32 bytes, but is an error log
// rather than a counter: it does not move with actuations. It is left out
// deliberately.
var models = []Model{
	{
		Name:        "EOS 5D Mark IV",
		ProductID:   0x3281,
		RatedLife:   150000,
		Confirmed:   true,
		ConfirmedOn: "firmware 1.2.1",
		Counters: []Counter{
			{Label: "Shutter actuations", Address: 0x1001, Length: 10},
			{Label: "Mirror actuations", Address: 0x100b, Length: 10},
			{Label: "Total counter", Address: 0x1015, Length: 10},
		},
	},
}

// Models returns every body with a known address map.
func Models() []Model {
	out := make([]Model, len(models))
	copy(out, models)

	return out
}

// LookupModel returns the address map for a USB product ID.
func LookupModel(productID uint16) (Model, bool) {
	for _, m := range models {
		if m.ProductID == productID {
			return m, true
		}
	}

	return Model{}, false
}

// tested are the bodies whose readings were worked out on the body itself.
// Any other camera that reads is labelled as not fully tested.
var tested = map[string]bool{
	"EOS 5D Mark IV": true,
	"EOS R8":         true,
}

// GuessModel is what a body that lists the factory channel but has no map of
// its own is read with: the 5D Mark IV's addresses. The channel itself is very
// likely the same across these bodies, but the addresses aren't known to be, so
// the reading says it's not fully tested. MonReadAndGetData only reads.
func GuessModel(productID uint16) Model {
	reference := models[0]

	return Model{
		Name:      fmt.Sprintf("Canon body (USB %04x:%04x)", VendorID, productID),
		ProductID: productID,
		Counters:  reference.Counters,
		Confirmed: false,
	}
}
