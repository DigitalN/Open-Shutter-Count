package usb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ioregSample is cut down from `ioreg -r -c IOUSBHostDevice -l -w0` on the
// maintainer's Mac (macOS 27.2) with an EOS R8 that another app had open,
// serial numbers left out, after a keyboard for contrast.
const ioregSample = `+-o Magic Keyboard@01200000  <class IOUSBHostDevice, id 0x100000a01, registered>
  | {
  |   "idProduct" = 615
  |   "idVendor" = 1452
  |   "UsbExclusiveOwner" = "pid 111, hidd"
  | }
  |
+-o Canon Digital Camera@01100000  <class IOUSBHostDevice, id 0x1000148fd, registered, matched, active>
  | {
  |   "USB Product Name" = "Canon Digital Camera"
  |   "idProduct" = 13068
  |   "idVendor" = 1193
  | }
  |
  +-o IOUSBHostInterface@0  <class IOUSBHostInterface, id 0x100014903, registered, matched, active>
  | | {
  | |   "bInterfaceClass" = 6
  | |   "UsbExclusiveOwner" = "pid 69966, ptpcamerad"
  | | }
  | |
  | +-o ptpcamerad  <class AppleUSBHostInterfaceUserClient, id 0x10001490e, !registered>
  | |     "IOUserClientCreator" = "pid 69966, ptpcamerad"
  | +-o openshuttercount  <class AppleUSBHostInterfaceUserClient, id 0x100014a9b, !registered>
  | |     "IOUserClientCreator" = "pid 74749, openshuttercount"
  | +-o openshuttercount  <class AppleUSBHostInterfaceUserClient, id 0x100014a9c, !registered>
  |       "IOUserClientCreator" = "pid 74749, openshuttercount"
  +-o iAP Interface@1  <class IOUSBHostInterface, id 0x100014904, registered, matched, active>
  | +-o accessoryd  <class AppleUSBHostInterfaceUserClient, id 0x100014909, !registered>
  |       "IOUserClientCreator" = "pid 73425, accessoryd"
  +-o camerashuttercount  <class AppleUSBHostDeviceUserClient, id 0x10001490a, !registered>
        "IOUserClientCreator" = "pid 69958, camerashuttercou"
`

func TestParseIORegOwners(t *testing.T) {
	owners, summary := ParseIORegOwners(ioregSample, 0x04a9, 0x330c)

	assert.Equal(t, []Owner{
		{PID: 69966, Name: "ptpcamerad", Exclusive: true},
		{PID: 74749, Name: "openshuttercount"},
		{PID: 73425, Name: "accessoryd"},
		{PID: 69958, Name: "camerashuttercount"},
	}, owners)

	assert.Equal(t, `IOUSBHostInterface IOUSBHostInterface@0: ptpcamerad (pid 69966, exclusive)
AppleUSBHostInterfaceUserClient ptpcamerad: ptpcamerad (pid 69966)
AppleUSBHostInterfaceUserClient openshuttercount: openshuttercount (pid 74749)
AppleUSBHostInterfaceUserClient accessoryd: accessoryd (pid 73425)
AppleUSBHostDeviceUserClient camerashuttercount: camerashuttercount (pid 69958)`, summary)
	assert.NotContains(t, summary, "hidd")
}

func TestParseIORegOwnersOtherDevice(t *testing.T) {
	owners, summary := ParseIORegOwners(ioregSample, 0x04a9, 0x32a6)

	assert.Empty(t, owners)
	assert.Empty(t, summary)
}

func TestParseIORegOwnersNobody(t *testing.T) {
	out := `+-o Canon Digital Camera@01100000  <class IOUSBHostDevice, id 0x1, registered>
  |   "idProduct" = 13068
  |   "idVendor" = 1193
`

	owners, summary := ParseIORegOwners(out, 0x04a9, 0x330c)
	assert.Empty(t, owners)
	assert.Empty(t, summary)
}

func TestOwnerString(t *testing.T) {
	assert.Equal(t, "Arc (pid 70060)", Owner{PID: 70060, Name: "Arc"}.String())
	assert.Equal(t, "ptpcamerad (pid 1, exclusive)", Owner{PID: 1, Name: "ptpcamerad", Exclusive: true}.String())
}
