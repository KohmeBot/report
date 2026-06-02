package invoker

import (
	"encoding/json"
	"fmt"
	"github.com/kohmebot/chatai/chatai/chataisdk"
	"github.com/kohmebot/chatai/chatai/model"
	"github.com/sirupsen/logrus"
	"reflect"
	"strings"
	"time"
)

type JsonInvoker struct {
	invoker  *chataisdk.ChatAIInvoker
	system   string
	online   bool
	thinking bool
}

func NewJsonInvoker(invoker *chataisdk.ChatAIInvoker, system string, online bool, thinking bool) *JsonInvoker {
	return &JsonInvoker{
		invoker:  invoker,
		system:   system,
		online:   online,
		thinking: thinking,
	}
}

func (i *JsonInvoker) DoRequest(req string, val any) error {
	var largeModel model.LargeModel
	var err error
	if i.system == "" {
		largeModel, err = i.invoker.NewDefaultModel(i.online, i.thinking, true)
	} else {
		largeModel, err = i.invoker.NewModel(i.system, i.online, i.thinking, true)
	}

	if err != nil {
		return err
	}

	var retry int
	var res string
	for {
		res, err = i.invoker.DoRequestWithModel(req, largeModel)
		if err != nil {
			return err
		}

		// 容错：AI可能在JSON外面加```json```
		res = strings.TrimSpace(res)
		res = strings.TrimPrefix(res, "```json")
		res = strings.TrimPrefix(res, "```")
		res = strings.TrimSuffix(res, "```")

		err = json.Unmarshal([]byte(res), val)

		if err == nil {
			// 使用反射检查val是否有空的值，如果有则重试
			err = valid(val)
			if err == nil {
				return nil
			}
		}

		logrus.Errorf("json validation error: %v", err)
		retry++
		if retry >= 3 {
			return fmt.Errorf("max retries exceeded, last validation error: %w", err)
		}
		// 三秒
		time.Sleep(3 * time.Second)
	}

}

func valid(val any) error {
	if val == nil {
		return fmt.Errorf("val is nil")
	}

	v := reflect.ValueOf(val)

	// val 必须是指针类型
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("val must be a pointer")
	}
	if v.IsNil() {
		return fmt.Errorf("val pointer is nil")
	}

	return validateValue(v.Elem(), "")
}

func validateValue(v reflect.Value, fieldName string) error {

	if v.CanInterface() {
		ip, ok := v.Interface().(interface{ IsEmpty() bool })
		if ok {
			if ip.IsEmpty() {
				return fmt.Errorf("val is empty")
			}
			return nil
		}
	}

	switch v.Kind() {
	case reflect.String:
		if v.String() == "" {
			if fieldName != "" {
				return fmt.Errorf("field '%s' is empty string", fieldName)
			}
			return fmt.Errorf("string value is empty")
		}

	case reflect.Slice:
		if v.IsNil() {
			if fieldName != "" {
				return fmt.Errorf("field '%s' is empty slice", fieldName)
			}
			return fmt.Errorf("slice is empty")
		}
		// 递归检查切片中的每个元素
		for j := 0; j < v.Len(); j++ {
			if err := validateValue(v.Index(j), fmt.Sprintf("%s[%d]", fieldName, j)); err != nil {
				return err
			}
		}

	case reflect.Struct:
		t := v.Type()
		for j := 0; j < v.NumField(); j++ {
			field := v.Field(j)
			ft := t.Field(j)

			// 跳过未导出字段
			if !ft.IsExported() {
				continue
			}
			// 跳过 json:"-"
			if tag := ft.Tag.Get("json"); tag == "-" {
				continue
			}

			name := ft.Name
			if fieldName != "" {
				name = fieldName + "." + name
			}

			if err := validateValue(field, name); err != nil {
				return err
			}
		}

	case reflect.Ptr:
		if v.IsNil() {
			if fieldName != "" {
				return fmt.Errorf("field '%s' is nil pointer", fieldName)
			}
			return fmt.Errorf("pointer is nil")
		}
		return validateValue(v.Elem(), fieldName)
	}

	return nil
}
