package main

import (
	"encoding/csv"
	"os"
	"strconv"
	"time"

	"gopkg.in/dedis/onet.v1/log"
)

func init() {
	layout = "2006-01-02 15:04:05"
	var err error
	first, err = time.Parse(layout, "2009-01-03 00:00:00")
	log.ErrFatal(err)
}

var layout string
var first time.Time

type interpolated struct {
	values []int64
	ppv    int
	first  int
}

var totalSize interpolated
var unspentSize interpolated

func (i *interpolated) GetValue(index int) int64 {
	floor := index / i.ppv
	if floor < 0 || floor >= len(i.values)+1 {
		return -1
	}
	part := int64(index - floor*i.ppv)
	this := i.getIndex(floor)
	next := i.getIndex(floor + 1)
	diff := next - this
	ret := this + diff*part/int64(i.ppv)
	//log.Print("index, ppv, floor, part, diff", index, i.ppv,
	//	floor, part, diff,
	//	ret, this, next)
	return ret
}

func (i *interpolated) getIndex(index int) int64 {
	if index < 0 || index >= len(i.values) {
		return -2
	}
	if val := i.values[index]; val >= 0 {
		return val
	}
	var firstDay int
	for firstDay = index; firstDay > 0; firstDay-- {
		if i.values[firstDay] >= 0 {
			break
		}
	}
	var lastDay int
	for lastDay = index; lastDay > 0; lastDay++ {
		if i.values[lastDay] >= 0 {
			break
		}
	}
	firstVal := i.values[firstDay]
	lastVal := i.values[lastDay]
	diffVal := lastVal - firstVal
	diffDay := int64(lastDay - firstDay)
	//log.Print(firstVal, lastVal, diffVal, diffDay)
	return firstVal + diffVal*(int64(index-firstDay))/diffDay
}

func readCSV(name string, ppv int, mul float64, monInc bool) (*interpolated, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	values, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	length := getDay(values[len(values)-1][0]) + 1
	ip := &interpolated{
		values: make([]int64, length),
		ppv:    ppv,
		first:  getDay(values[0][0]),
	}
	// Initialize with -1 to know which ones are not used
	for i := range ip.values[1:] {
		ip.values[i+1] = -1
	}
	if err != nil {
		return nil, err
	}
	var last int64
	for _, v := range values {
		val, err := strconv.ParseFloat(v[1], 64)
		current := int64(val * mul)
		if current < last && monInc {
			log.Warn("Decreasing value")
		}
		ip.values[getDay(v[0])] = current
		if err != nil {
			return nil, err
		}
		last = current
	}
	ip.values[len(ip.values)-1] = last
	return ip, nil
}

func getDay(t string) int {
	date, err := time.Parse(layout, t)
	log.ErrFatal(err)
	return int(date.Sub(first).Hours() / 24)
}
