package models

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/database"
)

type StudioQueryBuilder struct{}

func NewStudioQueryBuilder() StudioQueryBuilder {
	return StudioQueryBuilder{}
}

func (qb *StudioQueryBuilder) Create(newStudio Studio, tx *sqlx.Tx) (*Studio, error) {
	ensureTx(tx)
	result, err := tx.NamedExec(
		`INSERT INTO studios (image, checksum, name, url, created_at, updated_at)
				VALUES (:image, :checksum, :name, :url, :created_at, :updated_at)
		`,
		newStudio,
	)
	if err != nil {
		return nil, err
	}
	studioID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := tx.Get(&newStudio, `SELECT * FROM studios WHERE id = ? LIMIT 1`, studioID); err != nil {
		return nil, err
	}
	return &newStudio, nil
}

func (qb *StudioQueryBuilder) Update(updatedStudio Studio, tx *sqlx.Tx) (*Studio, error) {
	ensureTx(tx)
	_, err := tx.NamedExec(
		`UPDATE studios SET `+SQLGenKeys(updatedStudio)+` WHERE studios.id = :id`,
		updatedStudio,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Get(&updatedStudio, `SELECT * FROM studios WHERE id = ? LIMIT 1`, updatedStudio.ID); err != nil {
		return nil, err
	}
	return &updatedStudio, nil
}

func (qb *StudioQueryBuilder) Destroy(id string, tx *sqlx.Tx) error {
	// remove studio from scenes
	_, err := tx.Exec("UPDATE scenes SET studio_id = null WHERE studio_id = ?", id)
	if err != nil {
		return err
	}

	// remove studio from scraped items
	_, err = tx.Exec("UPDATE scraped_items SET studio_id = null WHERE studio_id = ?", id)
	if err != nil {
		return err
	}

	return executeDeleteQuery("studios", id, tx)
}

func (qb *StudioQueryBuilder) Find(id int, tx *sqlx.Tx) (*Studio, error) {
	query := "SELECT * FROM studios WHERE id = ? LIMIT 1"
	args := []interface{}{id}
	return qb.queryStudio(query, args, tx)
}

func (qb *StudioQueryBuilder) FindBySceneID(sceneID int) (*Studio, error) {
	query := "SELECT studios.* FROM studios JOIN scenes ON studios.id = scenes.studio_id WHERE scenes.id = ? LIMIT 1"
	args := []interface{}{sceneID}
	return qb.queryStudio(query, args, nil)
}

func (qb *StudioQueryBuilder) FindByName(name string, tx *sqlx.Tx) (*Studio, error) {
	query := "SELECT * FROM studios WHERE name = ? LIMIT 1"
	args := []interface{}{name}
	return qb.queryStudio(query, args, tx)
}

func (qb *StudioQueryBuilder) Count() (int, error) {
	return runCountQuery(buildCountQuery("SELECT studios.id FROM studios"), nil)
}

func (qb *StudioQueryBuilder) All() ([]*Studio, error) {
	return qb.queryStudios(selectAll("studios")+qb.getStudioSort(nil), nil, nil)
}

func (qb *StudioQueryBuilder) Query(findFilter *FindFilterType) ([]*Studio, int) {
	if findFilter == nil {
		findFilter = &FindFilterType{}
	}

	var whereClauses []string
	var havingClauses []string
	var args []interface{}
	body := selectDistinctIDs("studios")
	body += `
		left join scenes on studios.id = scenes.studio_id		
	`

	if q := findFilter.Q; q != nil && *q != "" {
		searchColumns := []string{"studios.name"}
		whereClauses = append(whereClauses, getSearch(searchColumns, *q))
	}

	sortAndPagination := qb.getStudioSort(findFilter) + getPagination(findFilter)
	idsResult, countResult := executeFindQuery("studios", body, args, sortAndPagination, whereClauses, havingClauses)

	var studios []*Studio
	for _, id := range idsResult {
		studio, _ := qb.Find(id, nil)
		studios = append(studios, studio)
	}

	return studios, countResult
}

func (qb *StudioQueryBuilder) getStudioSort(findFilter *FindFilterType) string {
	var sort string
	var direction string
	if findFilter == nil {
		sort = "name"
		direction = "ASC"
	} else {
		sort = findFilter.GetSort("name")
		direction = findFilter.GetDirection()
	}
	return getSort(sort, direction, "studios")
}

func (qb *StudioQueryBuilder) queryStudio(query string, args []interface{}, tx *sqlx.Tx) (*Studio, error) {
	results, err := qb.queryStudios(query, args, tx)
	if err != nil || len(results) < 1 {
		return nil, err
	}
	return results[0], nil
}

func (qb *StudioQueryBuilder) queryStudios(query string, args []interface{}, tx *sqlx.Tx) ([]*Studio, error) {
	var rows *sqlx.Rows
	var err error
	if tx != nil {
		rows, err = tx.Queryx(query, args...)
	} else {
		rows, err = database.DB.Queryx(query, args...)
	}

	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	defer rows.Close()

	studios := make([]*Studio, 0)
	for rows.Next() {
		studio := Studio{}
		if err := rows.StructScan(&studio); err != nil {
			return nil, err
		}
		studios = append(studios, &studio)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return studios, nil
}
