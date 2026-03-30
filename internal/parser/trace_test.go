package parser

import (
	"strings"
	"testing"
)

const testFixturePath = "../../testdata/simple.xt"

func TestParseFile_EntryCount(t *testing.T) {
	entries, err := ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Count by type
	var entryCount, exitCount, returnCount int
	for _, e := range entries {
		switch {
		case e.IsEntry:
			entryCount++
		case e.IsExit:
			exitCount++
		case e.IsReturn:
			returnCount++
		}
	}

	if entryCount != 10 {
		t.Errorf("expected 10 entry lines, got %d", entryCount)
	}
	if exitCount != 10 {
		t.Errorf("expected 10 exit lines, got %d", exitCount)
	}
	if returnCount != 5 {
		t.Errorf("expected 5 return lines, got %d", returnCount)
	}

	totalExpected := 25
	if len(entries) != totalExpected {
		t.Errorf("expected %d total entries, got %d", totalExpected, len(entries))
	}
}

func TestParseFile_EntryLine(t *testing.T) {
	entries, err := ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Second entry: ReservationController->create (index 1)
	e := entries[1]
	if !e.IsEntry {
		t.Fatal("expected entry line")
	}
	if e.Level != 2 {
		t.Errorf("expected level 2, got %d", e.Level)
	}
	if e.FunctionNr != 2 {
		t.Errorf("expected function_nr 2, got %d", e.FunctionNr)
	}
	if e.FunctionName != "App\\Controller\\ReservationController->create" {
		t.Errorf("unexpected function name: %s", e.FunctionName)
	}
	if !e.UserDefined {
		t.Error("expected user_defined to be true")
	}
	if e.Filename != "/var/www/app/src/Controller/ReservationController.php" {
		t.Errorf("unexpected filename: %s", e.Filename)
	}
	if e.LineNumber != 25 {
		t.Errorf("expected line 25, got %d", e.LineNumber)
	}
	if len(e.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(e.Params))
	}
	if e.Params[0] != "class Symfony\\Component\\HttpFoundation\\Request { ... }" {
		t.Errorf("unexpected param: %s", e.Params[0])
	}
}

func TestParseFile_ExitLine(t *testing.T) {
	entries, err := ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Find first exit line (Reservation->__construct exit)
	var exit *TraceEntry
	for i := range entries {
		if entries[i].IsExit && entries[i].FunctionNr == 4 {
			exit = &entries[i]
			break
		}
	}
	if exit == nil {
		t.Fatal("could not find exit line for function_nr 4")
	}
	if exit.Level != 4 {
		t.Errorf("expected level 4, got %d", exit.Level)
	}
	if exit.FunctionName != "" {
		t.Errorf("exit line should have empty function name, got %q", exit.FunctionName)
	}
	if exit.Memory != 403500 {
		t.Errorf("expected memory 403500, got %d", exit.Memory)
	}
}

func TestParseFile_ReturnLine(t *testing.T) {
	entries, err := ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Find return for ReservationRepository->save (function_nr 5)
	var ret *TraceEntry
	for i := range entries {
		if entries[i].IsReturn && entries[i].FunctionNr == 5 {
			ret = &entries[i]
			break
		}
	}
	if ret == nil {
		t.Fatal("could not find return line for function_nr 5")
	}
	if ret.ReturnValue != "TRUE" {
		t.Errorf("expected return value 'TRUE', got %q", ret.ReturnValue)
	}
}

func TestParseFile_InternalFunction(t *testing.T) {
	entries, err := ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// Find strlen call (function_nr 9)
	var strlen *TraceEntry
	for i := range entries {
		if entries[i].IsEntry && entries[i].FunctionNr == 9 {
			strlen = &entries[i]
			break
		}
	}
	if strlen == nil {
		t.Fatal("could not find strlen entry")
	}
	if strlen.FunctionName != "strlen" {
		t.Errorf("expected 'strlen', got %q", strlen.FunctionName)
	}
	if strlen.UserDefined {
		t.Error("strlen should not be user-defined")
	}
}

func TestParseFile_MultipleParams(t *testing.T) {
	entries, err := ParseFile(testFixturePath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// ReservationService->create has 2 params (function_nr 3)
	var svc *TraceEntry
	for i := range entries {
		if entries[i].IsEntry && entries[i].FunctionNr == 3 {
			svc = &entries[i]
			break
		}
	}
	if svc == nil {
		t.Fatal("could not find ReservationService->create entry")
	}
	if len(svc.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(svc.Params))
	}
	if svc.Params[0] != "'Hotel Marvelous'" {
		t.Errorf("unexpected param[0]: %s", svc.Params[0])
	}
	if svc.Params[1] != "'2024-06-15'" {
		t.Errorf("unexpected param[1]: %s", svc.Params[1])
	}
}

func TestParseStream_SkipsHeaderAndFooter(t *testing.T) {
	// A minimal trace with only header and footer, no data
	input := "Version: 3.1.0\nFile format: 4\nTRACE START [2024-01-01 00:00:00.000000]\n\nTRACE END   [2024-01-01 00:00:01.000000]\n"
	var count int
	err := ParseStream(strings.NewReader(input), func(e TraceEntry) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 entries from empty trace, got %d", count)
	}
}

func TestParseStream_MalformedLine(t *testing.T) {
	input := "Version: 3.1.0\nFile format: 4\nTRACE START [2024-01-01 00:00:00.000000]\nnot\ta\tvalid\nTRACE END   [2024-01-01 00:00:01.000000]\n"
	err := ParseStream(strings.NewReader(input), func(e TraceEntry) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error on malformed line")
	}
}

func TestParseStream_TruncatedHeader(t *testing.T) {
	input := "Version: 3.1.0\n"
	err := ParseStream(strings.NewReader(input), func(e TraceEntry) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error on truncated header")
	}
}
