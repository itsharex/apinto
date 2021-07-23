package apikey

import (
	"github.com/eolinker/eosc"
	"reflect"
)

const (
	driverName = "apikey"
)

//driver 实现github.com/eolinker/eosc.eosc.IProfessionDriver接口
type driver struct {
	profession string
	name       string
	driver     string
	label      string
	desc       string
	configType reflect.Type
	params     map[string]string
}

func (d *driver) ConfigType() reflect.Type {
	return d.configType
}

func (d *driver) Create(id, name string, v interface{}, workers map[eosc.RequireId]interface{}) (eosc.IWorker, error) {
	w := &apikey{
		id:     id,
		name:   name,
		driver: driverName,
	}
	err := w.Reset(v, workers)
	if err != nil {
		return nil, err
	}

	return w, nil
}
