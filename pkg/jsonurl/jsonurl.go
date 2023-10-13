package jsonurl

import (
	"fmt"
	"net/url"
	"database/sql/driver"
)

type JSONURL struct {
	url.URL
}

func (j *JSONURL) UnmarshalJSON(data []byte) error {
	url, err := url.Parse(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}

	j.URL = *url
	return nil
}

func (j JSONURL) MarshalJSON() ([]byte, error) {
	url := fmt.Sprintf("\"%s\"", j.URL.String())
	return []byte(url), nil
}

func (j *JSONURL) Scan(src interface{}) error {
	url, err := url.Parse(fmt.Sprint(src))
	if err != nil {
		return err
	}

	j.URL = *url
	return nil
}

func (j JSONURL) Value() (driver.Value, error) {
	return j.URL.String(), nil
}
