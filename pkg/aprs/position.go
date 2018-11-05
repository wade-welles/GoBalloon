// GoBalloon
// compressed.go - Functions for created and decoding compressed APRS position reports
//
// (c) 2014-2018, Christopher Snell

package aprs

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/chrissnell/GoBalloon/pkg/base91"
	"github.com/chrissnell/GoBalloon/pkg/geospatial"
)

// CreateUncompressedPositionReportWithoutTimestamp creates an APRS position report without a timestamp.
// The report is in a format suitable for adding to the data payload of an AX.25 APRS packet.
func CreateUncompressedPositionReportWithoutTimestamp(p geospatial.Point, symTable, symCode rune, messaging bool) (string, error) {
	var buffer bytes.Buffer
	var latitudeHemisphere, longitudeHemisphere rune

	if messaging {
		buffer.WriteRune('=')
	} else {
		buffer.WriteRune('!')
	}

	if math.Abs(p.Lat) > 90 {
		return "", fmt.Errorf("latitude is > +/- 90 degrees: %v", p.Lat)
	}

	if math.Abs(p.Lon) > 180 {
		return "", fmt.Errorf("longitude is > +/- 180 degrees: %v", p.Lon)
	}

	if p.Lat > 0 {
		latitudeHemisphere = 'N'
	} else {
		latitudeHemisphere = 'S'
	}

	if p.Lon > 0 {
		longitudeHemisphere = 'E'
	} else {
		longitudeHemisphere = 'W'
	}

	buffer.WriteString(geospatial.LatDecimalDegreesToDegreesDecimalMinutes(math.Abs(p.Lat)))
	buffer.WriteRune(latitudeHemisphere)

	buffer.WriteRune(symTable)

	buffer.WriteString(geospatial.LonDecimalDegreesToDegreesDecimalMinutes(math.Abs(p.Lon)))
	buffer.WriteRune(longitudeHemisphere)

	buffer.WriteRune(symCode)

	return buffer.String(), nil
}

// CreateCompressedPositionReport  creates an APRS position report in compressed format.
// The report is in a format suitable for adding to the data payload of an AX.25 APRS packet.
func CreateCompressedPositionReport(p geospatial.Point, symTable, symCode rune) string {
	var buffer bytes.Buffer

	// First byte in our compressed position report is the data type indicator.
	// The rune '!' indicates a real-time compressed position report
	buffer.WriteRune('!')

	// Next byte is the symbol table selector
	buffer.WriteRune(symTable)

	// Next four bytes is the decimal latitude, compressed with funky Base91
	buffer.WriteString(string(base91.EncodeBase91Position(int(base91.LatPrecompress(p.Lat)))))

	// Then comes the longitude, same compression
	buffer.WriteString(string(base91.EncodeBase91Position(int(base91.LonPrecompress(p.Lon)))))

	// Then our symbol code
	buffer.WriteRune(symCode)

	// Then we compress our altitude with a funky logrithm and conver to Base91
	buffer.Write(base91.AltitudeCompress(p.Altitude))

	// This last byte specifies: a live GPS fix, in GGA NMEA format, with the
	// compressed position generated by software (this program!).  See APRS
	// Protocol Reference v1.0, page 39, for more details on this wack shit.
	buffer.WriteByte(byte(0x32) + 33)

	return buffer.String()
}

// DecodeCompressedPositionReport decodes a compressed position report into a geospatial.Point
// and also returns the symbol table and code.
func DecodeCompressedPositionReport(c string) (geospatial.Point, rune, rune, string, error) {
	// Example:    =/5L!!<*e7OS]S

	var err error
	var matches []string

	p := geospatial.Point{}

	pr := regexp.MustCompile(`[=!]([\\\/])(.{4})(.{4})(.)(..)(.)(.*)$`)

	remains := pr.ReplaceAllString(c, "")

	p.Time = time.Now()

	if matches = pr.FindStringSubmatch(c); len(matches) > 0 {

		if len(matches[7]) > 0 {
			remains = matches[7]
		}

		symTable := rune(matches[1][0])
		symCode := rune(matches[4][0])

		p.Lat, err = base91.DecodeBase91Lat([]byte(matches[2]))
		if err != nil {
			return p, ' ', ' ', remains, fmt.Errorf("could not decode compressed latitude: %v", err)
		}

		p.Lon, err = base91.DecodeBase91Lon([]byte(matches[3]))
		if err != nil {
			return p, ' ', ' ', remains, fmt.Errorf("could not decode compressed longitude: %v", err)
		}

		// A space in this position indicates that the report includes no altitude, speed/course, or radio range.
		if matches[5][0] != ' ' {

			// First we look at the Compression Byte ("T" in the spec) and check for a GGA NMEA source.
			// If the GGA bits are set, we decode an altitude reading.  Otherwise, try to decode a course/
			// speed reading or a radio range reading
			if (byte(matches[6][0])-33)&0x18 == 0x10 {
				// This report has an encoded altitude reading
				p.Altitude, err = base91.DecodeBase91Altitude([]byte(matches[5]))
				if err != nil {
					return p, ' ', ' ', remains, fmt.Errorf("could not decode compressed altitude: %v", err)
				}
			} else if (byte(matches[5][0])-33) >= 0 && (byte(matches[5][0])-33) <= 89 {
				p.Heading, p.Speed, err = base91.DecodeBase91CourseSpeed([]byte(matches[5]))
			} else if matches[5][0] == '{' {
				p.RadioRange = base91.DecodeBase91RadioRange(byte(matches[5][1]))
			}
		}

		return p, symTable, symCode, remains, nil

	}
	return p, ' ', ' ', remains, nil
}

// DecodeUncompressedPositionReportWithoutTimestamp decodes an uncompressed position report that
// lacks a timestamp into a geospatial.Poiint and also returns the symbol table and code.
func DecodeUncompressedPositionReportWithoutTimestamp(c string) (geospatial.Point, rune, rune, string, error) {
	// Example:   !4903.50N/07201.75W-

	var matches []string
	p := geospatial.Point{}

	if len(c) >= 20 {

		pr := regexp.MustCompile(`([\=\!])([\d\.\s]{7})([NSns])(.)([\d\.\s]{8})([EWew])(.)(.*)$`)

		remains := pr.ReplaceAllString(c, "")

		p.Time = time.Now()

		if matches = pr.FindStringSubmatch(c); len(matches) > 0 {

			if len(matches[8]) > 0 {
				remains = matches[8]
			}

			if matches[1][0] == '=' {
				p.MessageCapable = true
			}

			symTable := rune(matches[4][0])
			symCode := rune(matches[7][0])

			la1, err := strconv.ParseFloat(matches[2][0:2], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}
			la2, err := strconv.ParseFloat(matches[2][2:7], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}

			lat := la1 + la2/60
			if matches[3][0] == 'S' {
				lat = 0 - lat
			}

			lo1, err := strconv.ParseFloat(matches[5][0:3], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}
			lo2, err := strconv.ParseFloat(matches[5][3:8], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}

			lon := lo1 + lo2/60

			if matches[6][0] == 'W' {
				lon = 0 - lon
			}

			p.Lat = lat
			p.Lon = lon

			return p, symTable, symCode, remains, nil

		}

	}

	return p, ' ', ' ', c, nil

}

// DecodeUncompressedPositionReportWithTimestamp decodes an uncompressed position report with a timestamp
// into a geospatial.Point and also returns the symbol table and code.
func DecodeUncompressedPositionReportWithTimestamp(c string) (geospatial.Point, rune, rune, string, error) {
	// Example:   @092345z4903.50N/07201.75W>

	var matches []string
	p := geospatial.Point{}

	if len(c) >= 27 {

		pr := regexp.MustCompile(`([\/\@])(\d{6})(.)([\d\.\s]{7})([NSns])(.)([\d\.\s]{8})([EWew])(.)(.*)$`)

		remains := pr.ReplaceAllString(c, "")

		if matches = pr.FindStringSubmatch(c); len(matches) > 0 {

			if len(matches[10]) > 0 {
				remains = matches[10]
			}

			if matches[1][0] == '@' {
				p.MessageCapable = true
			}

			switch matches[3][0] {
			case 'z':
				day, _ := strconv.ParseInt(matches[2][0:2], 10, 0)
				hours, _ := strconv.ParseInt(matches[2][2:4], 10, 0)
				minutes, _ := strconv.ParseInt(matches[2][4:6], 10, 0)
				now := time.Now()
				p.Time = time.Date(now.Year(), now.Month(), int(day), int(hours), int(minutes), 0, 0, time.UTC)
			case '/':
				day, _ := strconv.ParseInt(matches[2][0:2], 10, 0)
				hours, _ := strconv.ParseInt(matches[2][2:4], 10, 0)
				minutes, _ := strconv.ParseInt(matches[2][4:6], 10, 0)
				now := time.Now()
				p.Time = time.Date(now.Year(), now.Month(), int(day), int(hours), int(minutes), 0, 0, time.Local)
			case 'h':
				hours, _ := strconv.ParseInt(matches[2][0:2], 10, 0)
				minutes, _ := strconv.ParseInt(matches[2][2:4], 10, 0)
				seconds, _ := strconv.ParseInt(matches[2][4:6], 10, 0)
				now := time.Now()
				p.Time = time.Date(now.Year(), now.Month(), now.Day(), int(hours), int(minutes), int(seconds), 0, time.UTC)
			default:
				p.Time = time.Now()
			}

			symTable := rune(matches[6][0])
			symCode := rune(matches[9][0])

			la1, err := strconv.ParseFloat(matches[4][0:2], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}
			la2, err := strconv.ParseFloat(matches[4][2:7], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}

			lat := la1 + la2/60
			if matches[5][0] == 'S' {
				lat = 0 - lat
			}

			lo1, err := strconv.ParseFloat(matches[7][0:3], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}
			lo2, err := strconv.ParseFloat(matches[7][3:8], 64)
			if err != nil {
				return p, symTable, symCode, remains, err
			}

			lon := lo1 + lo2/60

			if matches[8][0] == 'W' {
				lon = 0 - lon
			}

			p.Lat = lat
			p.Lon = lon

			return p, symTable, symCode, remains, nil

		}

	}

	return p, ' ', ' ', c, nil

}
