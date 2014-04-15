package dbr

import (
	"fmt"
	"reflect"
	"time"
)

// Given a query and given a structure (field list), there's 2 sets of fields.
// Take the intersection. We can fill those in. great.
// For fields in the structure that aren't in the query, we'll let that slide if db:"-"
// For fields in the structure that aren't in the query but without db:"-", return error
// For fields in the query that aren't in the structure, we'll ignore them.

// dest can be:
// - addr of a structure
// - addr of slice of pointers to structures
// - map of pointers to structures (addr of map also ok)
// If it's a single structure, only the first record returned will be set.
// If it's a slice or map, the slice/map won't be emptied first. New records will be allocated for each found record.
// If its a map, there is the potential to overwrite values (keys are 'id')
// Returns the number of items found (which is not necessarily the # of items set)
func (sess *Session) SelectAll(dest interface{}, sql string, params ...interface{}) (int, error) {

	//
	// Validate the dest, and extract the reflection values we need.
	//
	valueOfDest := reflect.ValueOf(dest) // We want this to eventually be a map or slice
	kindOfDest := valueOfDest.Kind()     // And this eventually needs to be a map or slice as well

	if kindOfDest == reflect.Ptr {
		valueOfDest = reflect.Indirect(valueOfDest)
		kindOfDest = valueOfDest.Kind()
	} else if kindOfDest == reflect.Map {
		// we're good
	} else {
		panic("invalid type passed to AllBySql. Need a map or addr of slice")
	}

	if !(kindOfDest == reflect.Map || kindOfDest == reflect.Slice) {
		panic("invalid type passed to AllBySql. Need a map or addr of slice")
	}

	recordType := valueOfDest.Type().Elem()
	if recordType.Kind() != reflect.Ptr {
		panic("Elements need to be pointersto structures")
	}

	recordType = recordType.Elem()
	if recordType.Kind() != reflect.Struct {
		panic("Elements need to be pointers to structures")
	}

	//
	// Get full SQL
	//
	fullSql, err := Interpolate(sql, params)
	if err != nil {
		return 0, err
	}

	numberOfRowsReturned := 0

	// Start the timer:
	startTime := time.Now()
	defer func() {
		sess.TimingKv("dbr.select", time.Since(startTime).Nanoseconds(), map[string]string{"sql": fullSql})
	}()

	// Run the query:
	rows, err := sess.cxn.Db.Query(fullSql)
	if err != nil {
		fmt.Println("dbr.error.query") // Kvs{"error": err.String(), "sql": fullSql}
		return 0, err
	}

	// Iterate over rows
	if kindOfDest == reflect.Slice {
		sliceValue := valueOfDest
		for rows.Next() {
			// Create a new record to store our row:
			pointerToNewRecord := reflect.New(recordType)
			newRecord := reflect.Indirect(pointerToNewRecord)

			// Build a 'holder', which is an []interface{}. Each value will be the address of the field corresponding to our newly made record:
			holder, err := sess.holderFor(recordType, newRecord, rows)
			if err != nil {
				return numberOfRowsReturned, err
			}

			// Load up our new structure with the row's values
			err = rows.Scan(holder...)
			if err != nil {
				return numberOfRowsReturned, err
			}

			// Append our new record to the slice:
			sliceValue = reflect.Append(sliceValue, pointerToNewRecord)

			numberOfRowsReturned += 1
		}
		valueOfDest.Set(sliceValue)
	} else { // Map

	}

	// Check for errors at the end. Supposedly these are error that can happen during iteration.
	if err = rows.Err(); err != nil {
		return numberOfRowsReturned, err
	}

	return numberOfRowsReturned, nil
}

func (sess *Session) SelectOne(dest interface{}, sql string, params ...interface{}) (bool, error) {
	//
	// Validate the dest, and extract the reflection values we need.
	//
	valueOfDest := reflect.ValueOf(dest)
	indirectOfDest := reflect.Indirect(valueOfDest)
	kindOfDest := valueOfDest.Kind()

	if kindOfDest != reflect.Ptr || indirectOfDest.Kind() != reflect.Struct {
		panic("you need to pass in the address of a struct")
	}

	recordType := indirectOfDest.Type()

	//
	// Get full SQL
	//
	fullSql, err := Interpolate(sql, params)
	if err != nil {
		return false, err
	}

	// Start the timer:
	startTime := time.Now()
	defer func() { sess.TimingKv("dbr.select", time.Since(startTime).Nanoseconds(), kvs{"sql": sql}) }()

	// Run the query:
	rows, err := sess.cxn.Db.Query(fullSql)
	if err != nil {
		sess.EventErrKv("dbr.select_one.query.error", err, kvs{"sql": fullSql})
		return false, err
	}

	if rows.Next() {
		// Build a 'holder', which is an []interface{}. Each value will be the address of the field corresponding to our newly made record:
		holder, err := sess.holderFor(recordType, indirectOfDest, rows)
		if err != nil {
			return false, err
		}

		// Load up our new structure with the row's values
		err = rows.Scan(holder...)
		if err != nil {
			return false, err
		}

		return true, nil
	}

	if err := rows.Err(); err != nil {
		return false, err
	}

	return false, nil
}