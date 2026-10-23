package echo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"
)

// jsonSerializer is Echo's default JSON serializer, except that a request body
// it cannot decode yields a 400 whose message says what is wrong with the
// body. Echo's default answers every such body with a bare "Bad Request".
// The messages match the ones Echo v4 sent.
type jsonSerializer struct {
	echo.DefaultJSONSerializer
}

func (jsonSerializer) Deserialize(c *echo.Context, target any) error {
	err := json.NewDecoder(c.Request().Body).Decode(target)
	if err == nil {
		return nil
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		message := fmt.Sprintf("Unmarshal type error: expected=%v, got=%v, field=%v, offset=%v",
			typeErr.Type, typeErr.Value, typeErr.Field, typeErr.Offset)
		return echo.NewHTTPError(http.StatusBadRequest, message).Wrap(err)
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		message := fmt.Sprintf("Syntax error: offset=%v, error=%v", syntaxErr.Offset, syntaxErr.Error())
		return echo.NewHTTPError(http.StatusBadRequest, message).Wrap(err)
	}

	return echo.NewHTTPError(http.StatusBadRequest, err.Error()).Wrap(err)
}
