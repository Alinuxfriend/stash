// +build integration

package manager

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stashapp/stash/pkg/database"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
)

const testName = "Foo's Bar"
const testExtension = ".mp4"

var testSeparators = []string{
	".",
	"-",
	"_",
	" ",
}

func generateNamePatterns(name string, separator string) []string {
	var ret []string
	ret = append(ret, fmt.Sprintf("%s%saaa"+testExtension, name, separator))
	ret = append(ret, fmt.Sprintf("aaa%s%s"+testExtension, separator, name))
	ret = append(ret, fmt.Sprintf("aaa%s%s%sbbb"+testExtension, separator, name, separator))
	ret = append(ret, fmt.Sprintf("dir/%s%saaa"+testExtension, name, separator))
	ret = append(ret, fmt.Sprintf("dir\\%s%saaa"+testExtension, name, separator))
	ret = append(ret, fmt.Sprintf("%s%saaa/dir/bbb"+testExtension, name, separator))
	ret = append(ret, fmt.Sprintf("%s%saaa\\dir\\bbb"+testExtension, name, separator))
	ret = append(ret, fmt.Sprintf("dir/%s%s/aaa"+testExtension, name, separator))
	ret = append(ret, fmt.Sprintf("dir\\%s%s\\aaa"+testExtension, name, separator))

	return ret
}

func generateFalseNamePattern(name string, separator string) string {
	splitted := strings.Split(name, " ")

	return fmt.Sprintf("%s%saaa%s%s"+testExtension, splitted[0], separator, separator, splitted[1])
}

func testTeardown(databaseFile string) {
	err := database.DB.Close()

	if err != nil {
		panic(err)
	}

	err = os.Remove(databaseFile)
	if err != nil {
		panic(err)
	}
}

func runTests(m *testing.M) int {
	// create the database file
	f, err := ioutil.TempFile("", "*.sqlite")
	if err != nil {
		panic(fmt.Sprintf("Could not create temporary file: %s", err.Error()))
	}

	f.Close()
	databaseFile := f.Name()
	database.Initialize(databaseFile)

	// defer close and delete the database
	defer testTeardown(databaseFile)

	err = populateDB()
	if err != nil {
		panic(fmt.Sprintf("Could not populate database: %s", err.Error()))
	} else {
		// run the tests
		return m.Run()
	}
}

func TestMain(m *testing.M) {
	ret := runTests(m)
	os.Exit(ret)
}

func createPerformer(tx *sqlx.Tx) error {
	// create the performer
	pqb := models.NewPerformerQueryBuilder()

	performer := models.Performer{
		Image:    []byte{0, 1, 2},
		Checksum: testName,
		Name:     sql.NullString{Valid: true, String: testName},
		Favorite: sql.NullBool{Valid: true, Bool: false},
	}

	_, err := pqb.Create(performer, tx)
	if err != nil {
		return err
	}

	return nil
}

func createStudio(tx *sqlx.Tx) error {
	// create the studio
	qb := models.NewStudioQueryBuilder()

	studio := models.Studio{
		Image:    []byte{0, 1, 2},
		Checksum: testName,
		Name:     sql.NullString{Valid: true, String: testName},
	}

	_, err := qb.Create(studio, tx)
	if err != nil {
		return err
	}

	return nil
}

func createTag(tx *sqlx.Tx) error {
	// create the studio
	qb := models.NewTagQueryBuilder()

	tag := models.Tag{
		Name: testName,
	}

	_, err := qb.Create(tag, tx)
	if err != nil {
		return err
	}

	return nil
}

func createScenes(tx *sqlx.Tx) error {
	sqb := models.NewSceneQueryBuilder()

	// create the scenes
	var scenePatterns []string
	var falseScenePatterns []string
	for _, separator := range testSeparators {
		scenePatterns = append(scenePatterns, generateNamePatterns(testName, separator)...)
		scenePatterns = append(scenePatterns, generateNamePatterns(strings.ToLower(testName), separator)...)
		if separator != " " {
			scenePatterns = append(scenePatterns, generateNamePatterns(strings.Replace(testName, " ", separator, -1), separator)...)
		}
		falseScenePatterns = append(falseScenePatterns, generateFalseNamePattern(testName, separator))
	}

	for _, fn := range scenePatterns {
		err := createScene(sqb, tx, fn, true)
		if err != nil {
			return err
		}
	}
	for _, fn := range falseScenePatterns {
		err := createScene(sqb, tx, fn, false)
		if err != nil {
			return err
		}
	}

	return nil
}

func createScene(sqb models.SceneQueryBuilder, tx *sqlx.Tx, name string, expectedResult bool) error {
	scene := models.Scene{
		Checksum: utils.MD5FromString(name),
		Path:     name,
	}

	// if expectedResult is true then we expect it to match, set the title accordingly
	if expectedResult {
		scene.Title = sql.NullString{Valid: true, String: name}
	}

	_, err := sqb.Create(scene, tx)

	if err != nil {
		return fmt.Errorf("Failed to create scene with name '%s': %s", name, err.Error())
	}

	return nil
}

func populateDB() error {
	ctx := context.TODO()
	tx := database.DB.MustBeginTx(ctx, nil)

	err := createPerformer(tx)
	if err != nil {
		return err
	}

	err = createStudio(tx)
	if err != nil {
		return err
	}

	err = createTag(tx)
	if err != nil {
		return err
	}

	err = createScenes(tx)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func TestParsePerformers(t *testing.T) {
	pqb := models.NewPerformerQueryBuilder()
	performers, err := pqb.All()

	if err != nil {
		t.Errorf("Error getting performer: %s", err)
		return
	}

	task := AutoTagPerformerTask{
		performer: performers[0],
	}

	var wg sync.WaitGroup
	wg.Add(1)
	task.Start(&wg)

	// verify that scenes were tagged correctly
	sqb := models.NewSceneQueryBuilder()

	scenes, err := sqb.All()

	for _, scene := range scenes {
		performers, err := pqb.FindBySceneID(scene.ID, nil)

		if err != nil {
			t.Errorf("Error getting scene performers: %s", err.Error())
			return
		}

		// title is only set on scenes where we expect performer to be set
		if scene.Title.String == scene.Path && len(performers) == 0 {
			t.Errorf("Did not set performer '%s' for path '%s'", testName, scene.Path)
		} else if scene.Title.String != scene.Path && len(performers) > 0 {
			t.Errorf("Incorrectly set performer '%s' for path '%s'", testName, scene.Path)
		}
	}
}

func TestParseStudios(t *testing.T) {
	studioQuery := models.NewStudioQueryBuilder()
	studios, err := studioQuery.All()

	if err != nil {
		t.Errorf("Error getting studio: %s", err)
		return
	}

	task := AutoTagStudioTask{
		studio: studios[0],
	}

	var wg sync.WaitGroup
	wg.Add(1)
	task.Start(&wg)

	// verify that scenes were tagged correctly
	sqb := models.NewSceneQueryBuilder()

	scenes, err := sqb.All()

	for _, scene := range scenes {
		// title is only set on scenes where we expect studio to be set
		if scene.Title.String == scene.Path && scene.StudioID.Int64 != int64(studios[0].ID) {
			t.Errorf("Did not set studio '%s' for path '%s'", testName, scene.Path)
		} else if scene.Title.String != scene.Path && scene.StudioID.Int64 == int64(studios[0].ID) {
			t.Errorf("Incorrectly set studio '%s' for path '%s'", testName, scene.Path)
		}
	}
}

func TestParseTags(t *testing.T) {
	tagQuery := models.NewTagQueryBuilder()
	tags, err := tagQuery.All()

	if err != nil {
		t.Errorf("Error getting performer: %s", err)
		return
	}

	task := AutoTagTagTask{
		tag: tags[0],
	}

	var wg sync.WaitGroup
	wg.Add(1)
	task.Start(&wg)

	// verify that scenes were tagged correctly
	sqb := models.NewSceneQueryBuilder()

	scenes, err := sqb.All()

	for _, scene := range scenes {
		tags, err := tagQuery.FindBySceneID(scene.ID, nil)

		if err != nil {
			t.Errorf("Error getting scene tags: %s", err.Error())
			return
		}

		// title is only set on scenes where we expect performer to be set
		if scene.Title.String == scene.Path && len(tags) == 0 {
			t.Errorf("Did not set tag '%s' for path '%s'", testName, scene.Path)
		} else if scene.Title.String != scene.Path && len(tags) > 0 {
			t.Errorf("Incorrectly set tag '%s' for path '%s'", testName, scene.Path)
		}
	}
}
