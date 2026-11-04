package usb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ioregSample is shaped like `ioreg -r -c IOUSBHostDevice -l -w0`: a keyboard,
// then an EOS R8 that ptpcamerad has open, with an interface and a user
// client below it.
const ioregSample = `+-o Magic Keyboard@01200000  <class IOUSBHostDevice, id 0x100000a01, registered, matched, active>
  | {
  |   "idProduct" = 615
  |   "idVendor" = 1452
  |   "UsbExclusiveOwner" = "pid 111, hidd"
  | }
  |
+-o Canon Digital Camera@01100000  <class IOUSBHostDevice, id 0x100000b02, registered, matched, active, busy 0 (2 ms), retain 30>
  | {
  |   "USB Product Name" = "Canon Digital Camera"
  |   "idProduct" = 13068
  |   "idVendor" = 1193
  |   "UsbExclusiveOwner" = "pid 589, ptpcamerad"
  | }
  |
  +-o AppleUSBHostDeviceUserClient  <class AppleUSBHostDeviceUserClient, id 0x100000b03, !registered>
  |   {
  |     "IOUserClientCreator" = "pid 589, ptpcamerad"
  |   }
  |
  +-o Canon Digital Camera@0  <class IOUSBHostInterface, id 0x100000b04, registered, matched, active, busy 0 (0 ms), retain 8>
      {
        "bInterfaceClass" = 6
        "UsbExclusiveOwner" = "pid 777, Some Camera App"
      }
`

func TestParseIORegOwners(t *testing.T) {
	owners, block := ParseIORegOwners(ioregSample, 0x04a9, 0x330c)

	assert.Equal(t, []Owner{{589, "ptpcamerad"}, {777, "Some Camera App"}}, owners)
	assert.Contains(t, block, "Canon Digital Camera")
	assert.NotContains(t, block, "hidd")
}

func TestParseIORegOwnersOtherDevice(t *testing.T) {
	owners, block := ParseIORegOwners(ioregSample, 0x04a9, 0x32a6)

	assert.Empty(t, owners)
	assert.Empty(t, block)
}

func TestParseIORegOwnersNobody(t *testing.T) {
	out := `+-o Canon Digital Camera@01100000  <class IOUSBHostDevice, id 0x1, registered>
  |   "idProduct" = 13068
  |   "idVendor" = 1193
`

	owners, block := ParseIORegOwners(out, 0x04a9, 0x330c)
	assert.Empty(t, owners)
	assert.NotEmpty(t, block)
}

func TestOwnerString(t *testing.T) {
	assert.Equal(t, "ptpcamerad (pid 589)", Owner{589, "ptpcamerad"}.String())
}
