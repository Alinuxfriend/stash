package models

import (
	"database/sql"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/database"
	"github.com/stashapp/stash/pkg/logger"
)

type queryBuilder struct {
	tableName string
	body      string

	whereClauses  []string
	havingClauses []string
	args          []interface{}

	sortAndPagination string
}

func (qb queryBuilder) executeFind() ([]int, int) {
	return executeFindQuery(qb.tableName, qb.body, qb.args, qb.sortAndPagination, qb.whereClauses, qb.havingClauses)
}

func (qb *queryBuilder) addWhere(clauses ...string) {
	qb.whereClauses = append(qb.whereClauses, clauses...)
}

func (qb *queryBuilder) addHaving(clauses ...string) {
	qb.havingClauses = append(qb.havingClauses, clauses...)
}

func (qb *queryBuilder) addArg(args ...interface{}) {
	qb.args = append(qb.args, args...)
}

var randomSortFloat = rand.Float64()

func selectAll(tableName string) string {
	idColumn := getColumn(tableName, "*")
	return "SELECT " + idColumn + " FROM " + tableName + " "
}

func selectDistinctIDs(tableName string) string {
	idColumn := getColumn(tableName, "id")
	return "SELECT DISTINCT " + idColumn + " FROM " + tableName + " "
}

func buildCountQuery(query string) string {
	return "SELECT COUNT(*) as count FROM (" + query + ") as temp"
}

func getColumn(tableName string, columnName string) string {
	return tableName + "." + columnName
}

func getPagination(findFilter *FindFilterType) string {
	if findFilter == nil {
		panic("nil find filter for pagination")
	}

	var page int
	if findFilter.Page == nil || *findFilter.Page < 1 {
		page = 1
	} else {
		page = *findFilter.Page
	}

	var perPage int
	if findFilter.PerPage == nil {
		perPage = 25
	} else {
		perPage = *findFilter.PerPage
	}
	if perPage > 120 {
		perPage = 120
	} else if perPage < 1 {
		perPage = 1
	}

	page = (page - 1) * perPage
	return " LIMIT " + strconv.Itoa(perPage) + " OFFSET " + strconv.Itoa(page) + " "
}

func getSort(sort string, direction string, tableName string) string {
	if direction != "ASC" && direction != "DESC" {
		direction = "ASC"
	}

	if strings.Contains(sort, "_count") {
		var relationTableName = strings.Split(sort, "_")[0] // TODO: pluralize?
		colName := getColumn(relationTableName, "id")
		return " ORDER BY COUNT(distinct " + colName + ") " + direction
	} else if strings.Compare(sort, "filesize") == 0 {
		colName := getColumn(tableName, "size")
		return " ORDER BY cast(" + colName + " as integer) " + direction
	} else if strings.Compare(sort, "random") == 0 {
		// https://stackoverflow.com/a/24511461
		// TODO seed as a parameter from the UI
		colName := getColumn(tableName, "id")
		randomSortString := strconv.FormatFloat(randomSortFloat, 'f', 16, 32)
		return " ORDER BY " + "(substr(" + colName + " * " + randomSortString + ", length(" + colName + ") + 2))" + " " + direction
	} else {
		colName := getColumn(tableName, sort)
		var additional string
		if tableName == "scenes" {
			additional = ", bitrate DESC, framerate DESC, rating DESC, duration DESC"
		} else if tableName == "scene_markers" {
			additional = ", scene_markers.scene_id ASC, scene_markers.seconds ASC"
		}
		if strings.Compare(sort, "name") == 0 {
			return " ORDER BY " + colName + " COLLATE NOCASE " + direction + additional
		}

		return " ORDER BY " + colName + " " + direction + additional
	}
}

func getSearch(columns []string, q string) string {
	// TODO - susceptible to SQL injection
	var likeClauses []string
	queryWords := strings.Split(q, " ")
	trimmedQuery := strings.Trim(q, "\"")
	if trimmedQuery == q {
		// Search for any word
		for _, word := range queryWords {
			for _, column := range columns {
				likeClauses = append(likeClauses, column+" LIKE '%"+word+"%'")
			}
		}
	} else {
		// Search the exact query
		for _, column := range columns {
			likeClauses = append(likeClauses, column+" LIKE '%"+trimmedQuery+"%'")
		}
	}
	likes := strings.Join(likeClauses, " OR ")

	return "(" + likes + ")"
}

func getSearchBinding(columns []string, q string, not bool) (string, []interface{}) {
	var likeClauses []string
	var args []interface{}

	notStr := ""
	binaryType := " OR "
	if not {
		notStr = " NOT "
		binaryType = " AND "
	}

	queryWords := strings.Split(q, " ")
	trimmedQuery := strings.Trim(q, "\"")
	if trimmedQuery == q {
		// Search for any word
		for _, word := range queryWords {
			for _, column := range columns {
				likeClauses = append(likeClauses, column+notStr+" LIKE ?")
				args = append(args, "%"+word+"%")
			}
		}
	} else {
		// Search the exact query
		for _, column := range columns {
			likeClauses = append(likeClauses, column+notStr+" LIKE ?")
			args = append(args, "%"+trimmedQuery+"%")
		}
	}
	likes := strings.Join(likeClauses, binaryType)

	return "(" + likes + ")", args
}

func getInBinding(length int) string {
	bindings := strings.Repeat("?, ", length)
	bindings = strings.TrimRight(bindings, ", ")
	return "(" + bindings + ")"
}

func getCriterionModifierBinding(criterionModifier CriterionModifier, value interface{}) (string, int) {
	var length int
	switch x := value.(type) {
	case []string:
		length = len(x)
	case []int:
		length = len(x)
	default:
		length = 1
	}
	if modifier := criterionModifier.String(); criterionModifier.IsValid() {
		switch modifier {
		case "EQUALS", "NOT_EQUALS", "GREATER_THAN", "LESS_THAN", "IS_NULL", "NOT_NULL":
			return getSimpleCriterionClause(criterionModifier, "?")
		case "INCLUDES":
			return "IN " + getInBinding(length), length // TODO?
		case "EXCLUDES":
			return "NOT IN " + getInBinding(length), length // TODO?
		default:
			logger.Errorf("todo")
			return "= ?", 1 // TODO
		}
	}
	return "= ?", 1 // TODO
}

func getSimpleCriterionClause(criterionModifier CriterionModifier, rhs string) (string, int) {
	if modifier := criterionModifier.String(); criterionModifier.IsValid() {
		switch modifier {
		case "EQUALS":
			return "= " + rhs, 1
		case "NOT_EQUALS":
			return "!= " + rhs, 1
		case "GREATER_THAN":
			return "> " + rhs, 1
		case "LESS_THAN":
			return "< " + rhs, 1
		case "IS_NULL":
			return "IS NULL", 0
		case "NOT_NULL":
			return "IS NOT NULL", 0
		default:
			logger.Errorf("todo")
			return "= ?", 1 // TODO
		}
	}

	return "= ?", 1 // TODO
}

func getIntCriterionWhereClause(column string, input IntCriterionInput) (string, int) {
	binding, count := getCriterionModifierBinding(input.Modifier, input.Value)
	return column + " " + binding, count
}

func runIdsQuery(query string, args []interface{}) ([]int, error) {
	var result []struct {
		Int int `db:"id"`
	}
	if err := database.DB.Select(&result, query, args...); err != nil && err != sql.ErrNoRows {
		return []int{}, err
	}

	vsm := make([]int, len(result))
	for i, v := range result {
		vsm[i] = v.Int
	}
	return vsm, nil
}

func runCountQuery(query string, args []interface{}) (int, error) {
	// Perform query and fetch result
	result := struct {
		Int int `db:"count"`
	}{0}
	if err := database.DB.Get(&result, query, args...); err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	return result.Int, nil
}

func executeFindQuery(tableName string, body string, args []interface{}, sortAndPagination string, whereClauses []string, havingClauses []string) ([]int, int) {
	if len(whereClauses) > 0 {
		body = body + " WHERE " + strings.Join(whereClauses, " AND ") // TODO handle AND or OR
	}
	body = body + " GROUP BY " + tableName + ".id "
	if len(havingClauses) > 0 {
		body = body + " HAVING " + strings.Join(havingClauses, " AND ") // TODO handle AND or OR
	}

	countQuery := buildCountQuery(body)
	countResult, countErr := runCountQuery(countQuery, args)

	idsQuery := body + sortAndPagination
	idsResult, idsErr := runIdsQuery(idsQuery, args)

	if countErr != nil {
		logger.Errorf("Error executing count query with SQL: %s, args: %v, error: %s", countQuery, args, countErr.Error())
		panic(countErr)
	}
	if idsErr != nil {
		logger.Errorf("Error executing find query with SQL: %s, args: %v, error: %s", idsQuery, args, idsErr.Error())
		panic(idsErr)
	}

	return idsResult, countResult
}

func executeDeleteQuery(tableName string, id string, tx *sqlx.Tx) error {
	if tx == nil {
		panic("must use a transaction")
	}
	idColumnName := getColumn(tableName, "id")
	_, err := tx.Exec(
		`DELETE FROM `+tableName+` WHERE `+idColumnName+` = ?`,
		id,
	)
	return err
}

func ensureTx(tx *sqlx.Tx) {
	if tx == nil {
		panic("must use a transaction")
	}
}

// https://github.com/jmoiron/sqlx/issues/410
// sqlGenKeys is used for passing a struct and returning a string
// of keys for non empty key:values. These keys are formated
// keyname=:keyname with a comma seperating them
func SQLGenKeys(i interface{}) string {
	return sqlGenKeys(i, false)
}

// support a partial interface. When a partial interface is provided,
// keys will always be included if the value is not null. The partial
// interface must therefore consist of pointers
func SQLGenKeysPartial(i interface{}) string {
	return sqlGenKeys(i, true)
}

func sqlGenKeys(i interface{}, partial bool) string {
	var query []string
	v := reflect.ValueOf(i)
	for i := 0; i < v.NumField(); i++ {
		//get key for struct tag
		rawKey := v.Type().Field(i).Tag.Get("db")
		key := strings.Split(rawKey, ",")[0]
		if key == "id" {
			continue
		}
		switch t := v.Field(i).Interface().(type) {
		case string:
			if partial || t != "" {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case int:
			if partial || t != 0 {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case float64:
			if partial || t != 0 {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case SQLiteTimestamp:
			if partial || !t.Timestamp.IsZero() {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case SQLiteDate:
			if partial || t.Valid {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case sql.NullString:
			if partial || t.Valid {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case sql.NullBool:
			if partial || t.Valid {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case sql.NullInt64:
			if partial || t.Valid {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		case sql.NullFloat64:
			if partial || t.Valid {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		default:
			reflectValue := reflect.ValueOf(t)
			isNil := reflectValue.IsNil()
			if !isNil {
				query = append(query, fmt.Sprintf("%s=:%s", key, key))
			}
		}
	}
	return strings.Join(query, ", ")
}
