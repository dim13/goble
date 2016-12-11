package goble

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dim13/goble/uname"
	"github.com/dim13/goble/xpc"
)

//go:generate stringer -type State

// https://developer.apple.com/reference/corebluetooth/cbperipheralmanagerstate
type State int

const (
	unknown State = iota
	resetting
	unsupported
	unauthorized
	poweredOff
	poweredOn
)

// https://developer.apple.com/reference/corebluetooth/cbcharacteristicproperties
type Property int

const (
	Broadcast Property = 1 << iota
	Read
	WriteWithoutResponse
	Write
	Notify
	Indicate
	AuthenticatedSignedWrites
	ExtendedProperties
)

func (p Property) Readable() bool {
	return (p & Read) != 0
}

func (p Property) String() string {
	var result []string
	if (p & Broadcast) != 0 {
		result = append(result, "broadcast")
	}
	if (p & Read) != 0 {
		result = append(result, "read")
	}
	if (p & WriteWithoutResponse) != 0 {
		result = append(result, "writeWithoutResponse")
	}
	if (p & Write) != 0 {
		result = append(result, "write")
	}
	if (p & Notify) != 0 {
		result = append(result, "notify")
	}
	if (p & Indicate) != 0 {
		result = append(result, "indicate")
	}
	if (p & AuthenticatedSignedWrites) != 0 {
		result = append(result, "authenticateSignedWrites")
	}
	if (p & ExtendedProperties) != 0 {
		result = append(result, "extendedProperties")
	}

	return strings.Join(result, " ")
}

type ServiceData struct {
	Uuid string
	Data []byte
}

type CharacteristicDescriptor struct {
	Uuid   string
	Handle int
}

type ServiceCharacteristic struct {
	Uuid        string
	Name        string
	Type        string
	Properties  Property
	Descriptors map[interface{}]*CharacteristicDescriptor
	Handle      int
	ValueHandle int
}

type ServiceHandle struct {
	Uuid            string
	Name            string
	Type            string
	Characteristics map[interface{}]*ServiceCharacteristic
	startHandle     int
	endHandle       int
}

type Advertisement struct {
	LocalName        string
	TxPowerLevel     int
	ManufacturerData []byte
	ServiceData      []ServiceData
	ServiceUuids     []string
}

type Peripheral struct {
	Uuid          xpc.UUID
	Address       string
	AddressType   string
	Connectable   bool
	Advertisement Advertisement
	Rssi          int
	Services      map[interface{}]*ServiceHandle
}

// GATT Descriptor
type Descriptor struct {
	uuid  xpc.UUID
	value []byte
}

// GATT Characteristic
type Characteristic struct {
	uuid        xpc.UUID
	properties  Property
	secure      Property
	descriptors []Descriptor
	value       []byte
}

// GATT Service
type Service struct {
	uuid            xpc.UUID
	characteristics []Characteristic
}

type BLE struct {
	Emitter
	conn    xpc.XPC
	verbose bool

	peripherals            map[string]*Peripheral
	attributes             xpc.Array
	lastServiceAttributeId int
	allowDuplicates        bool
}

func New() *BLE {
	ble := &BLE{peripherals: map[string]*Peripheral{}, Emitter: Emitter{}}
	ble.Emitter.Init()
	ble.conn = xpc.XpcConnect("com.apple.blued", ble)
	return ble
}

func (ble *BLE) SetVerbose(v bool) {
	ble.verbose = v
	ble.Emitter.SetVerbose(v)
}

// events
// FIXME: source of magic values?
const (
	stateChangeEvt             = 6
	advertisingStartEvt        = 16
	advertisingStopEvt         = 17
	discoverEvt                = 37
	connectEvt                 = 38
	disconnectEvt              = 40
	mtuChangeEvt               = 53
	rssiUpdateEvt              = 54
	serviceDiscoverEvt         = 55
	characteristicsDiscoverEvt = 63
	descriptorDiscoverEvt      = 75
	readEvt                    = 70
)

// process BLE events and asynchronous errors
// (implements XpcEventHandler)
func (ble *BLE) HandleXpcEvent(event xpc.Dict, err error) {
	if err != nil {
		log.Println("error:", err)
		if event == nil {
			return
		}
	}

	id := event.MustGetInt("kCBMsgId")
	args := event.MustGetDict("kCBMsgArgs")

	if ble.verbose {
		log.Printf("event: %v %#v\n", id, args)
	}

	switch id {
	case stateChangeEvt:
		state := args.MustGetInt("kCBMsgArgState")
		ble.Emit(Event{
			Name:  "stateChange",
			State: State(state).String(),
		})

	case advertisingStartEvt:
		result := args.MustGetInt("kCBMsgArgResult")
		if result != 0 {
			log.Printf("event: error in advertisingStart %v\n", result)
		} else {
			ble.Emit(Event{
				Name: "advertisingStart",
			})
		}

	case advertisingStopEvt:
		result := args.MustGetInt("kCBMsgArgResult")
		if result != 0 {
			log.Printf("event: error in advertisingStop %v\n", result)
		} else {
			ble.Emit(Event{
				Name: "advertisingStop",
			})
		}

	case discoverEvt:
		advdata := args.MustGetDict("kCBMsgArgAdvertisementData")
		if len(advdata) == 0 {
			//log.Println("event: discover with no advertisment data")
			break
		}

		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")

		advertisement := Advertisement{
			LocalName:        advdata.GetString("kCBAdvDataLocalName", args.GetString("kCBMsgArgName", "")),
			TxPowerLevel:     advdata.GetInt("kCBAdvDataTxPowerLevel", 0),
			ManufacturerData: advdata.GetBytes("kCBAdvDataManufacturerData", nil),
			ServiceData:      []ServiceData{},
			ServiceUuids:     []string{},
		}

		connectable := advdata.GetInt("kCBAdvDataIsConnectable", 0) > 0
		rssi := args.GetInt("kCBMsgArgRssi", 0)

		if uuids, ok := advdata["kCBAdvDataServiceUUIDs"]; ok {
			for _, uuid := range uuids.(xpc.Array) {
				advertisement.ServiceUuids = append(advertisement.ServiceUuids, fmt.Sprintf("%x", uuid))
			}
		}

		if data, ok := advdata["kCBAdvDataServiceData"]; ok {
			sdata := data.(xpc.Array)

			for i := 0; i < len(sdata); i += 2 {
				sd := ServiceData{
					Uuid: fmt.Sprintf("%x", sdata[i+0].([]byte)),
					Data: sdata[i+1].([]byte),
				}

				advertisement.ServiceData = append(advertisement.ServiceData, sd)
			}
		}

		pid := deviceUuid.String()
		p := ble.peripherals[pid]
		emit := ble.allowDuplicates || p == nil

		if p == nil {
			// add new peripheral
			p = &Peripheral{
				Uuid:          deviceUuid,
				Connectable:   connectable,
				Advertisement: advertisement,
				Rssi:          rssi,
				Services:      map[interface{}]*ServiceHandle{},
			}

			ble.peripherals[pid] = p
		} else {
			// update peripheral
			p.Advertisement = advertisement
			p.Rssi = rssi
		}

		if emit {
			ble.Emit(Event{
				Name:       "discover",
				DeviceUUID: deviceUuid,
				Peripheral: *p,
			})
		}

	case connectEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		ble.Emit(Event{
			Name:       "connect",
			DeviceUUID: deviceUuid,
		})

	case disconnectEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		ble.Emit(Event{
			Name:       "disconnect",
			DeviceUUID: deviceUuid,
		})

	case mtuChangeEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		mtu := args.MustGetInt("kCBMsgArgATTMTU")

		// bleno here converts the deviceUuid to an address
		if p, ok := ble.peripherals[deviceUuid.String()]; ok {
			ble.Emit(Event{
				Name:       "mtuChange",
				DeviceUUID: deviceUuid,
				Peripheral: *p,
				Mtu:        mtu,
			})
		}

	case rssiUpdateEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		rssi := args.MustGetInt("kCBMsgArgData")

		if p, ok := ble.peripherals[deviceUuid.String()]; ok {
			p.Rssi = rssi
			ble.Emit(Event{
				Name:       "rssiUpdate",
				DeviceUUID: deviceUuid,
				Peripheral: *p,
			})
		}

	case serviceDiscoverEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		servicesUuids := []string{}
		servicesHandles := map[interface{}]*ServiceHandle{}

		if dservices, ok := args["kCBMsgArgServices"]; ok {
			for _, s := range dservices.(xpc.Array) {
				service := s.(xpc.Dict)
				serviceHandle := ServiceHandle{
					Uuid:            service.MustGetHexBytes("kCBMsgArgUUID"),
					startHandle:     service.MustGetInt("kCBMsgArgServiceStartHandle"),
					endHandle:       service.MustGetInt("kCBMsgArgServiceEndHandle"),
					Characteristics: map[interface{}]*ServiceCharacteristic{},
				}

				if nameType, ok := knownServices[serviceHandle.Uuid]; ok {
					serviceHandle.Name = nameType.Name
					serviceHandle.Type = nameType.Type
				}

				servicesHandles[serviceHandle.Uuid] = &serviceHandle
				servicesHandles[serviceHandle.startHandle] = &serviceHandle

				servicesUuids = append(servicesUuids, serviceHandle.Uuid)
			}
		}

		if p, ok := ble.peripherals[deviceUuid.String()]; ok {
			p.Services = servicesHandles
			ble.Emit(Event{
				Name:       "servicesDiscover",
				DeviceUUID: deviceUuid,
				Peripheral: *p,
			})
		}

	case characteristicsDiscoverEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		serviceStartHandle := args.MustGetInt("kCBMsgArgServiceStartHandle")

		if p, ok := ble.peripherals[deviceUuid.String()]; ok {
			service := p.Services[serviceStartHandle]

			//result := args.MustGetInt("kCBMsgArgResult")

			for _, c := range args.MustGetArray("kCBMsgArgCharacteristics") {
				cDict := c.(xpc.Dict)

				characteristic := ServiceCharacteristic{
					Uuid:        cDict.MustGetHexBytes("kCBMsgArgUUID"),
					Handle:      cDict.MustGetInt("kCBMsgArgCharacteristicHandle"),
					ValueHandle: cDict.MustGetInt("kCBMsgArgCharacteristicValueHandle"),
					Descriptors: map[interface{}]*CharacteristicDescriptor{},
				}

				if nameType, ok := knownCharacteristics[characteristic.Uuid]; ok {
					characteristic.Name = nameType.Name
					characteristic.Type = nameType.Type
				}

				properties := cDict.MustGetInt("kCBMsgArgCharacteristicProperties")
				characteristic.Properties = Property(properties)

				if service != nil {
					service.Characteristics[characteristic.Uuid] = &characteristic
					service.Characteristics[characteristic.Handle] = &characteristic
					service.Characteristics[characteristic.ValueHandle] = &characteristic
				}
			}

			if service != nil {
				ble.Emit(Event{
					Name:        "characteristicsDiscover",
					DeviceUUID:  deviceUuid,
					ServiceUuid: service.Uuid,
					Peripheral:  *p,
				})
			} else {
				log.Println("no service", serviceStartHandle)
			}
		} else {
			log.Println("no peripheral", deviceUuid)
		}

	case descriptorDiscoverEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		characteristicsHandle := args.MustGetInt("kCBMsgArgCharacteristicHandle")
		//result := args.MustGetInt("kCBMsgArgResult")

		if p, ok := ble.peripherals[deviceUuid.String()]; ok {
			for _, s := range p.Services {
				if c, ok := s.Characteristics[characteristicsHandle]; ok {
					for _, d := range args.MustGetArray("kCBMsgArgDescriptors") {
						dDict := d.(xpc.Dict)
						descriptor := CharacteristicDescriptor{
							Uuid:   dDict.MustGetHexBytes("kCBMsgArgUUID"),
							Handle: dDict.MustGetInt("kCBMsgArgDescriptorHandle"),
						}

						c.Descriptors[descriptor.Uuid] = &descriptor
						c.Descriptors[descriptor.Handle] = &descriptor
					}

					ble.Emit(Event{
						Name:               "descriptorsDiscover",
						DeviceUUID:         deviceUuid,
						ServiceUuid:        s.Uuid,
						CharacteristicUuid: c.Uuid,
						Peripheral:         *p,
					})
					break
				}
			}
		} else {
			log.Println("no peripheral", deviceUuid)
		}

	case readEvt:
		deviceUuid := args.MustGetUUID("kCBMsgArgDeviceUUID")
		characteristicsHandle := args.MustGetInt("kCBMsgArgCharacteristicHandle")
		//result := args.MustGetInt("kCBMsgArgResult")
		isNotification := args.GetInt("kCBMsgArgIsNotification", 0) != 0
		data := args.MustGetBytes("kCBMsgArgData")

		if p, ok := ble.peripherals[deviceUuid.String()]; ok {
			for _, s := range p.Services {
				if c, ok := s.Characteristics[characteristicsHandle]; ok {
					ble.Emit(Event{
						Name:               "read",
						DeviceUUID:         deviceUuid,
						ServiceUuid:        s.Uuid,
						CharacteristicUuid: c.Uuid,
						Peripheral:         *p,
						Data:               data,
						IsNotification:     isNotification,
					})
					break
				}
			}
		}
	}
}

// send a message to Blued
func (ble *BLE) sendCBMsg(id int, args xpc.Dict) {
	message := xpc.Dict{
		"kCBMsgId":   id,
		"kCBMsgArgs": args,
	}
	if ble.verbose {
		log.Printf("sendCBMsg %#v\n", message)
	}
	ble.conn.Send(message, ble.verbose)
}

// FIXME: source of magic values?
const (
	initMsg                    = 1
	startAdvertisingMsg        = 8
	stopAdvertisingMsg         = 9
	startScanningMsg           = 29
	stopScanningMsg            = 30
	connectMsg                 = 31
	disconnectMsg              = 32
	updateRssiMsg              = 43
	discoverServicesMsg        = 44
	discoverCharacteristicsMsg = 61
	discoverDescriptorsMsg     = 69
	readMsg                    = 64
	removeServicesMsg          = 12
	setServicesMsg             = 10
)

// initialize BLE
func (ble *BLE) Init() {
	ble.sendCBMsg(initMsg, xpc.Dict{
		"kCBMsgArgName":    fmt.Sprintf("goble-%v", time.Now().Unix()),
		"kCBMsgArgOptions": xpc.Dict{"kCBInitOptionShowPowerAlert": 0},
		"kCBMsgArgType":    0,
	})
}

// start advertising
func (ble *BLE) StartAdvertising(name string, serviceUuids []xpc.UUID) {
	uuids := make([][]byte, len(serviceUuids))
	for i, uuid := range serviceUuids {
		uuids[i] = []byte(uuid[:])
	}
	ble.sendCBMsg(startAdvertisingMsg, xpc.Dict{
		"kCBAdvDataLocalName":    name,
		"kCBAdvDataServiceUUIDs": uuids,
	})
}

// start advertising as IBeacon (raw data)
func (ble *BLE) StartAdvertisingIBeaconData(data []byte) {
	var utsname uname.Utsname
	uname.Uname(&utsname)

	// BUG: Why this hack?
	if utsname.Release >= "14." {
		l := len(data)
		buf := bytes.NewBuffer([]byte{byte(l + 5), 0xFF, 0x4C, 0x00, 0x02, byte(l)})
		buf.Write(data)
		ble.sendCBMsg(startAdvertisingMsg, xpc.Dict{
			"kCBAdvDataAppleMfgData": buf.Bytes(),
		})
	} else {
		ble.sendCBMsg(startAdvertisingMsg, xpc.Dict{
			"kCBAdvDataAppleBeaconKey": data,
		})
	}
}

// start advertising as IBeacon
func (ble *BLE) StartAdvertisingIBeacon(uuid xpc.UUID, major, minor uint16, measuredPower int8) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uuid[:])
	binary.Write(&buf, binary.BigEndian, major)
	binary.Write(&buf, binary.BigEndian, minor)
	binary.Write(&buf, binary.BigEndian, measuredPower)

	ble.StartAdvertisingIBeaconData(buf.Bytes())
}

// stop advertising
func (ble *BLE) StopAdvertising() {
	ble.sendCBMsg(stopAdvertisingMsg, nil)
}

// start scanning
func (ble *BLE) StartScanning(serviceUuids []xpc.UUID, allowDuplicates bool) {
	uuids := []string{}

	for _, uuid := range serviceUuids {
		uuids = append(uuids, uuid.String())
	}

	args := xpc.Dict{"kCBMsgArgUUIDs": uuids}
	if allowDuplicates {
		args["kCBMsgArgOptions"] = xpc.Dict{"kCBScanOptionAllowDuplicates": 1}
	} else {
		args["kCBMsgArgOptions"] = xpc.Dict{}
	}

	ble.allowDuplicates = allowDuplicates
	ble.sendCBMsg(startScanningMsg, args)
}

// stop scanning
func (ble *BLE) StopScanning() {
	ble.sendCBMsg(stopScanningMsg, nil)
}

// connect
func (ble *BLE) Connect(deviceUuid xpc.UUID) {
	uuid := deviceUuid.String()
	if p, ok := ble.peripherals[uuid]; ok {
		ble.sendCBMsg(connectMsg, xpc.Dict{
			"kCBMsgArgOptions":    xpc.Dict{"kCBConnectOptionNotifyOnDisconnection": 1},
			"kCBMsgArgDeviceUUID": p.Uuid,
		})
	} else {
		log.Println("no peripheral", deviceUuid)
	}
}

// disconnect
func (ble *BLE) Disconnect(deviceUuid xpc.UUID) {
	uuid := deviceUuid.String()
	if p, ok := ble.peripherals[uuid]; ok {
		ble.sendCBMsg(disconnectMsg, xpc.Dict{
			"kCBMsgArgDeviceUUID": p.Uuid,
		})
	} else {
		log.Println("no peripheral", deviceUuid)
	}
}

// update rssi
func (ble *BLE) UpdateRssi(deviceUuid xpc.UUID) {
	uuid := deviceUuid.String()
	if p, ok := ble.peripherals[uuid]; ok {
		ble.sendCBMsg(updateRssiMsg, xpc.Dict{
			"kCBMsgArgDeviceUUID": p.Uuid,
		})
	} else {
		log.Println("no peripheral", deviceUuid)
	}
}

// discover services
func (ble *BLE) DiscoverServices(deviceUuid xpc.UUID, uuids []xpc.UUID) {
	sUuid := deviceUuid.String()
	if p, ok := ble.peripherals[sUuid]; ok {
		sUuids := make([]string, len(uuids))
		for i, uuid := range uuids {
			sUuids[i] = uuid.String() // uuids may be a list of []byte (2 bytes)
		}
		ble.sendCBMsg(discoverServicesMsg, xpc.Dict{
			"kCBMsgArgDeviceUUID": p.Uuid,
			"kCBMsgArgUUIDs":      sUuids,
		})
	} else {
		log.Println("no peripheral", deviceUuid)
	}
}

// discover characteristics
func (ble *BLE) DiscoverCharacterstics(deviceUuid xpc.UUID, serviceUuid string, characteristicUuids []string) {
	sUuid := deviceUuid.String()
	if p, ok := ble.peripherals[sUuid]; ok {
		cUuids := make([]string, len(characteristicUuids))
		for i, cuuid := range characteristicUuids {
			cUuids[i] = cuuid // characteristicUuids may be a list of []byte (2 bytes)
		}

		ble.sendCBMsg(discoverCharacteristicsMsg, xpc.Dict{
			"kCBMsgArgDeviceUUID":         p.Uuid,
			"kCBMsgArgServiceStartHandle": p.Services[serviceUuid].startHandle,
			"kCBMsgArgServiceEndHandle":   p.Services[serviceUuid].endHandle,
			"kCBMsgArgUUIDs":              cUuids,
		})

	} else {
		log.Println("no peripheral", deviceUuid)
	}
}

// discover descriptors
func (ble *BLE) DiscoverDescriptors(deviceUuid xpc.UUID, serviceUuid, characteristicUuid string) {
	sUuid := deviceUuid.String()
	if p, ok := ble.peripherals[sUuid]; ok {
		s := p.Services[serviceUuid]
		c := s.Characteristics[characteristicUuid]

		ble.sendCBMsg(discoverDescriptorsMsg, xpc.Dict{
			"kCBMsgArgDeviceUUID":                p.Uuid,
			"kCBMsgArgCharacteristicHandle":      c.Handle,
			"kCBMsgArgCharacteristicValueHandle": c.ValueHandle,
		})
	} else {
		log.Println("no peripheral", deviceUuid)
	}
}

// read
func (ble *BLE) Read(deviceUuid xpc.UUID, serviceUuid, characteristicUuid string) {
	sUuid := deviceUuid.String()
	if p, ok := ble.peripherals[sUuid]; ok {
		s := p.Services[serviceUuid]
		c := s.Characteristics[characteristicUuid]

		ble.sendCBMsg(readMsg, xpc.Dict{
			"kCBMsgArgDeviceUUID":                p.Uuid,
			"kCBMsgArgCharacteristicHandle":      c.Handle,
			"kCBMsgArgCharacteristicValueHandle": c.ValueHandle,
		})
	} else {
		log.Println("no peripheral", deviceUuid)
	}
}

// remove all services
func (ble *BLE) RemoveServices() {
	ble.sendCBMsg(removeServicesMsg, nil)
}

// set services
func (ble *BLE) SetServices(services []Service) {
	ble.RemoveServices()
	ble.attributes = xpc.Array{nil}

	attributeId := 1

	for _, service := range services {
		arg := xpc.Dict{
			"kCBMsgArgAttributeID":     attributeId,
			"kCBMsgArgAttributeIDs":    []int{},
			"kCBMsgArgCharacteristics": nil,
			"kCBMsgArgType":            1, // 1 => primary, 0 => excluded
			"kCBMsgArgUUID":            service.uuid.String(),
		}

		ble.attributes = append(ble.attributes, service)
		ble.lastServiceAttributeId = attributeId
		attributeId += 1

		characteristics := xpc.Array{}

		for _, characteristic := range service.characteristics {
			properties := 0
			permissions := 0

			if Read&characteristic.properties != 0 {
				properties |= 0x02

				if Read&characteristic.secure != 0 {
					permissions |= 0x04
				} else {
					permissions |= 0x01
				}
			}

			if WriteWithoutResponse&characteristic.properties != 0 {
				properties |= 0x04

				if WriteWithoutResponse&characteristic.secure != 0 {
					permissions |= 0x08
				} else {
					permissions |= 0x02
				}
			}

			if Write&characteristic.properties != 0 {
				properties |= 0x08

				if WriteWithoutResponse&characteristic.secure != 0 {
					permissions |= 0x08
				} else {
					permissions |= 0x02
				}
			}

			if Notify&characteristic.properties != 0 {
				if Notify&characteristic.secure != 0 {
					properties |= 0x100
				} else {
					properties |= 0x10
				}
			}

			if Indicate&characteristic.properties != 0 {
				if Indicate&characteristic.secure != 0 {
					properties |= 0x200
				} else {
					properties |= 0x20
				}
			}

			descriptors := xpc.Array{}
			for _, descriptor := range characteristic.descriptors {
				descriptors = append(descriptors, xpc.Dict{"kCBMsgArgData": descriptor.value, "kCBMsgArgUUID": descriptor.uuid.String()})
			}

			characteristicArg := xpc.Dict{
				"kCBMsgArgAttributeID":              attributeId,
				"kCBMsgArgAttributePermissions":     permissions,
				"kCBMsgArgCharacteristicProperties": properties,
				"kCBMsgArgData":                     characteristic.value,
				"kCBMsgArgDescriptors":              descriptors,
				"kCBMsgArgUUID":                     characteristic.uuid.String(),
			}

			ble.attributes = append(ble.attributes, characteristic)
			characteristics = append(characteristics, characteristicArg)

			attributeId += 1
		}

		arg["kCBMsgArgCharacteristics"] = characteristics
		ble.sendCBMsg(setServicesMsg, arg) // remove all services
	}
}
