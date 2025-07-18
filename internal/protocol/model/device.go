package model

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/pkg/errors"
)

type DeviceStatus int8

const (
	DeviceStatusOffline  DeviceStatus = 0
	DeviceStatusOnline   DeviceStatus = 1
	DeviceStatusSleeping DeviceStatus = 2
)

// 终端设备的基础属性信息，用于数据缓存、持久化和保活相关流程处理
type Device struct {
	ID    string `json:"id"` // ID是否可重复？
	Plate string `json:"plate"`
	Phone string `json:"phone"` // 默认通过PhoneNumber来索引设备

	// 连接信息

	SessionID      string            `json:"sessionId"`
	TransProto     TransportProtocol `json:"transProto"`
	Conn           net.Conn          `json:"-"`
	Keepalive      time.Duration     `json:"keepalive"`   // 保活时长
	LastestComTime time.Time         `json:"lastComTime"` // 最近一次交互时间
	Status         DeviceStatus      `json:"status"`

	// 设备信息

	VersionDesc     VersionType `json:"versionDesc"`     // jt808协议版本描述, 区分 2011 / 2013 / 2019
	ProtocolVersion uint8       `json:"protocolVersion"` // jt808协议版本定义, 区分 (2011&2013) / 2019后续版本修订
	AuthCode        string      `json:"authcode"`
	IMEI            string      `json:"imei"`
	SoftwareVersion string      `json:"softwareVersion"` // 终端软件版本号(非jt808协议版本)
}

func NewDevice(in *Msg0100, session *Session) *Device {
	return &Device{
		ID:              in.DeviceID,
		Plate:           in.PlateNumber,
		Phone:           in.Header.PhoneNumber,
		SessionID:       session.ID,
		TransProto:      session.GetTransProto(),
		Conn:            session.Conn,
		Keepalive:       time.Minute * 1,
		LastestComTime:  time.Now(),
		Status:          DeviceStatusOffline,
		VersionDesc:     in.Header.Attr.VersionDesc,
		ProtocolVersion: in.Header.ProtocolVersion,
	}
}

func (d *Device) ShouleTurnOffline() bool {
	now := time.Now().UnixMilli()
	return d.Status != DeviceStatusOffline && now > d.Keepalive.Milliseconds()+d.LastestComTime.UnixMilli()
}

func (d *Device) ShouldClear() bool {
	now := time.Now().UnixMilli()
	return d.Status == DeviceStatusOffline && now > d.Keepalive.Milliseconds()+d.LastestComTime.UnixMilli()
}

// 终端设备地理位置状态相关信息
type DeviceGeo struct {
	Phone    string    `json:"phone"`
	Geo      *GeoMeta  `json:"gis"`
	Location *Location `json:"location"`
	Drive    *Drive    `json:"drive"`
	Time     time.Time `json:"time"`

	WifiInfos []*WifiInfo `json:"wifiInfos"` //为空可为nil
	LBSInfos  []*LBSInfo  `json:"lbsInfos"`  //为空可为nil
	Battery   *Battery    `json:"battery"`   //电池信息
	CsqLevel  int8        `json:"csq"`       // 信号强度(百分比)
	Sattelite int8        `json:"satellite"` // 卫星数量
}

type Battery struct {
	BatteryLevel int8 `json:"batteryLevel"` // 电池电量
	Charging     bool `json:"charging"`     // 充电状态
}

type WifiInfo struct {
	MAC  string `json:"mac"`
	RSSI int8   `json:"rssi"` //信号强度
}

type WifiList []*WifiInfo
type LBSList []*LBSInfo

type LBSInfo struct {
	MCC    uint16 `json:"mcc"`  // 移动国家码
	MNC    uint8  `json:"mnc"`  // 移动网络码
	LAC    uint16 `json:"lac"`  // 位置区码
	CellID uint32 `json:"ci"`   // 小区ID
	RSSI   int8   `json:"rssi"` // 信号强度
}

func (b *Battery) Decode(data []byte) error {
	if len(data) < 2 {
		return errors.New("empty battery data")
	}

	b.Charging = data[0] == 1
	b.BatteryLevel = int8(data[1])

	return nil
}

// WIFI列表解码方法
func (wifis *WifiList) Decode(data []byte) error {
	if len(data) < 1 {
		return errors.New("empty wifi data")
	}

	apCount := int(data[0])
	if len(data) != 1+apCount*7 {
		return fmt.Errorf("invalid wifi data length, expect %d, got %d",
			1+apCount*7, len(data))
	}

	*wifis = make([]*WifiInfo, apCount)

	for i := 0; i < apCount; i++ {
		offset := 1 + i*7
		apData := data[offset : offset+7]

		wifi := &WifiInfo{
			RSSI: byteToDBM(apData[0]),
			MAC: fmt.Sprintf("%02X%02X%02X%02X%02X%02X",
				apData[0], apData[1], apData[2],
				apData[3], apData[4], apData[5]),
		}

		(*wifis)[i] = wifi
	}

	return nil
}

// LBS列表解码方法
func (lbss *LBSList) Decode(data []byte) error {
	if len(data) < 1 {
		return errors.New("empty wifi data")
	}

	cellCount := int(data[0])
	if len(data) != 1+cellCount*10 {
		return fmt.Errorf("invalid lbs data length, expect %d, got %d",
			1+cellCount*10, len(data))
	}

	*lbss = make([]*LBSInfo, cellCount)

	for i := 0; i < cellCount; i++ {
		offset := 1 + i*10
		lbsData := data[offset : offset+10]

		lbs := &LBSInfo{
			MCC:    uint16(lbsData[0])<<8 | uint16(lbsData[1]),
			MNC:    uint8(lbsData[2]),
			LAC:    uint16(lbsData[3])<<8 | uint16(lbsData[4]),
			CellID: uint32(binary.BigEndian.Uint32(lbsData[5:9])), //大端序
			RSSI:   int8(lbsData[9]),
		}

		(*lbss)[i] = lbs
	}

	return nil
}

// 将十六进制字节字符串（如"26"、"5D"）转换为dBm值
func byteToDBM(b byte) int8 {
	unsignedByte := uint8(b)

	// 线性映射公式：dBm = -90 + (unsignedByte/255)*60
	dBm := -90.0 + (float64(unsignedByte)/255.0)*60.0
	return int8(dBm)
}

func (dg *DeviceGeo) Decode(phone string, m *Msg0200) error {
	dg.Phone = phone
	geoMetaInstance := &GeoMeta{}
	geoMetaInstance.Decode(m.StatusSign)
	dg.Geo = geoMetaInstance
	locInstance := &Location{}
	locInstance.Decode(m)

	dg.Location = locInstance
	driveInstance := &Drive{}
	driveInstance.Decode(m)
	dg.Drive = driveInstance
	dg.Time = hex.ParseTime(m.Time)

	if data, exists := m.AttachData[0x54]; exists && len(data) >= 2 { //WIFI
		var wifis WifiList = []*WifiInfo{}
		wifis.Decode(data)
		dg.WifiInfos = wifis
	}
	if data, exists := m.AttachData[0x5D]; exists && len(data) >= 2 { //LBS
		var LBSs LBSList = []*LBSInfo{}
		LBSs.Decode(data)
		dg.LBSInfos = LBSs
	}

	if data, exists := m.AttachData[0x04]; exists && len(data) >= 2 { //电量
		dg.Battery = &Battery{}
		dg.Battery.Decode(data)
	}

	if data, exists := m.AttachData[0x30]; exists && len(data) >= 1 { //网络信号
		dg.CsqLevel = int8(data[0])
	}
	if data, exists := m.AttachData[0x31]; exists && len(data) >= 1 { //卫星数量
		dg.Sattelite = int8(data[0])
	}

	return nil
}

const (
	LocationAccuracy = 1000000
	SpeedAccuracy    = 10
)

type Location struct {
	Latitude  float64 `json:"latitude"`  // 纬度，精确到百万分之一度
	Longitude float64 `json:"longitude"` // 精度，精确到百万分之一度
	Altitude  uint16  `json:"altitude"`  // 高程，海拔高度，单位为米(m)
}

func (l *Location) Decode(m *Msg0200) {
	l.Latitude = float64(m.Latitude) / LocationAccuracy
	l.Longitude = float64(m.Longitude) / LocationAccuracy
	l.Altitude = m.Altitude
}

type Drive struct {
	Speed     float64 `json:"speed"`     // 速度，单位为公里每小时, 精度0.1km/h
	Direction uint16  `json:"direction"` // 方向，0-359，正北为 0，顺时针
}

func (d *Drive) Decode(m *Msg0200) {
	d.Speed = float64(m.Speed) / SpeedAccuracy
	d.Direction = m.Direction
}

// 地理位置信息状态位字段的bit位
const (
	accBit                    uint32 = 0b00000000000000000000000000000001
	locationStatusBit         uint32 = 0b00000000000000000000000000000010
	LatitudeTypeBit           uint32 = 0b00000000000000000000000000000100
	LongitudeTypeBit          uint32 = 0b00000000000000000000000000001000
	operatingStatusBit        uint32 = 0b00000000000000000000000000010000
	gisEncryptionStatusBit    uint32 = 0b00000000000000000000000000100000
	loadStatusBit             uint32 = 0b00000000000000000000001100000000
	fuelSystemStatusBit       uint32 = 0b00000000000000000000010000000000
	alternatorSystemStatusBit uint32 = 0b00000000000000000000100000000000
	doorLockedStatusBit       uint32 = 0b00000000000000000001000000000000
	frontDoorStatusBit        uint32 = 0b00000000000000000010000000000000
	midDoorStatusBit          uint32 = 0b00000000000000000100000000000000
	backDoorStatusBit         uint32 = 0b00000000000000001000000000000000
	driverDoorStatusBit       uint32 = 0b00000000000000010000000000000000
	customDoorStatusBit       uint32 = 0b00000000000000100000000000000000
	gpsLocationStatusBit      uint32 = 0b00000000000001000000000000000000
	beidouLocatlonStatusBit   uint32 = 0b00000000000010000000000000000000
	glonassLocationStatusBit  uint32 = 0b00000000000100000000000000000000
	galileoLocationStatusBit  uint32 = 0b00000000001000000000000000000000
	drivingStatusBit          uint32 = 0b00000000010000000000000000000000
)

type GeoMeta struct {
	ACCStatus           uint8 `json:"accStatus"`           // bit0, 0:ACC 关;1: ACC 开
	LocationStatus      uint8 `json:"locationStatus"`      // bit1, 0:未定位;1:定位
	LatitudeType        uint8 `json:"latitudeType"`        // bit2, 0:北纬;1:南纬
	LongitudeType       uint8 `json:"longitudeType"`       // bit3, 0:东经;1:西经
	OperatingStatus     uint8 `json:"operatingStatus"`     // bit4, 0:运营状态;1:停运状态
	GeoEncryptionStatus uint8 `json:"geoEncryptionStatus"` // bit5, 0:经纬度未经保密插件加密;1:经纬度已经保密插件加密

	// bit6-7位保留

	LoadStatus             uint8 `json:"loadStatus"`             // bit8-9, 00:空车;01:半载;10:保留;11:满载 (可用于客车的空、重车及货车的空载、满载状态表示，人工输入或传感器获取)
	FuelSystemStatus       uint8 `json:"FuelSystemStatus"`       // bit10, 0:车辆油路正常;1:车辆油路断开
	AlternatorSystemStatus uint8 `json:"AlternatorSystemStatus"` // bit11, 0:车辆电路正常;1:车辆电路断开
	DoorLockedStatus       uint8 `json:"DoorLockedStatus"`       // bit12, 0:车门解锁;1:车门加锁
	FrontDoorStatus        uint8 `json:"frontDoorStatus"`        // bit13, 0:门1关;1:门1开(前门)
	MidDoorStatus          uint8 `json:"midDoorStatus"`          // bit14, 0:门2关;1:门2开(中门)
	BackDoorStatus         uint8 `json:"backDoorStatus"`         // bit15, 0:门3关;1:门3开(后门)
	DriverDoorStatus       uint8 `json:"driverDoorStatus"`       // bit16, 0:门4关;1:门4开(驾驶席门)
	CustomDoorStatus       uint8 `json:"customDoorStatus"`       // bit17, 0:门5关;1:门5开(自定义)
	GPSLocationStatus      uint8 `json:"gpsLocationStatus"`      // bit18, 0:未使用 GPS 卫星进行定位;1:使用 GPS 卫星进行定位
	BeidouLocationStatus   uint8 `json:"beidouLocationStatus"`   // bit19, 0:未使用北斗卫星进行定位;1:使用北斗卫星进行定位
	GLONASSLocationStatus  uint8 `json:"glonassLocationStatus"`  // bit20, 0:未使用 GLONASS 卫星进行定位;1:使用 GLONASS 卫星进行定位
	GalileoLocationStatus  uint8 `json:"galileoLocationStatus"`  // bit21, 0:未使用 Galileo 卫星进行定位;1:使用 Galileo 卫星进行定位
	DrivingStatus          uint8 `json:"drivingStatus"`          // bit22, 0:车辆处于停止状态;1:车辆处于行驶状态

	// bit23-31位保留
}

// 输入Msg0200的Status，按照协议解码geoMeta结构体
func (g *GeoMeta) Decode(status uint32) {
	g.ACCStatus = uint8(status & accBit)
	g.LocationStatus = uint8((status & locationStatusBit) >> 1)
	g.LatitudeType = uint8((status & LatitudeTypeBit) >> 2)
	g.LongitudeType = uint8((status & LongitudeTypeBit) >> 3)
	g.OperatingStatus = uint8((status & operatingStatusBit) >> 4)
	g.GeoEncryptionStatus = uint8((status & gisEncryptionStatusBit) >> 5)
	g.LoadStatus = uint8((status & loadStatusBit) >> 8)
	g.FuelSystemStatus = uint8((status & fuelSystemStatusBit) >> 10)
	g.AlternatorSystemStatus = uint8((status & alternatorSystemStatusBit) >> 11)
	g.DoorLockedStatus = uint8((status & doorLockedStatusBit) >> 12)
	g.FrontDoorStatus = uint8((status & frontDoorStatusBit) >> 13)
	g.MidDoorStatus = uint8((status & midDoorStatusBit) >> 14)
	g.BackDoorStatus = uint8((status & backDoorStatusBit) >> 15)
	g.DriverDoorStatus = uint8((status & driverDoorStatusBit) >> 16)
	g.CustomDoorStatus = uint8((status & customDoorStatusBit) >> 17)
	g.GPSLocationStatus = uint8((status & gpsLocationStatusBit) >> 18)
	g.BeidouLocationStatus = uint8((status & beidouLocatlonStatusBit) >> 19)
	g.GLONASSLocationStatus = uint8((status & glonassLocationStatusBit) >> 20)
	g.GalileoLocationStatus = uint8((status & galileoLocationStatusBit) >> 21)
	g.DrivingStatus = uint8((status & drivingStatusBit) >> 22)
}

func (g *GeoMeta) Encode() uint32 {
	var bitNum uint32
	bitNum += uint32(g.ACCStatus)
	bitNum += uint32(g.LocationStatus) << 1
	bitNum += uint32(g.LongitudeType) << 2
	bitNum += uint32(g.LatitudeType) << 3
	bitNum += uint32(g.OperatingStatus) << 4
	bitNum += uint32(g.GeoEncryptionStatus) << 5
	bitNum += uint32(g.LoadStatus) << 8
	bitNum += uint32(g.FuelSystemStatus) << 10
	bitNum += uint32(g.AlternatorSystemStatus) << 11
	bitNum += uint32(g.DoorLockedStatus) << 12
	bitNum += uint32(g.FrontDoorStatus) << 13
	bitNum += uint32(g.MidDoorStatus) << 14
	bitNum += uint32(g.BackDoorStatus) << 15
	bitNum += uint32(g.DriverDoorStatus) << 16
	bitNum += uint32(g.CustomDoorStatus) << 17
	bitNum += uint32(g.GPSLocationStatus) << 18
	bitNum += uint32(g.BeidouLocationStatus) << 19
	bitNum += uint32(g.GLONASSLocationStatus) << 20
	bitNum += uint32(g.GalileoLocationStatus) << 21
	bitNum += uint32(g.DrivingStatus) << 22
	return bitNum
}

type alarmMeta struct {
}
