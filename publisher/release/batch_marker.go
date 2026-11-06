package release

import (
	"fmt"
)

// BatchMarker is a Marker that can also end the markers of a whole batch at once.
// It lives next to Marker, which is left as it is, so single-app runs are not affected.
type BatchMarker interface {
	Marker
	// EndBatch ends the marks started, in order, by one batch run
	EndBatch(marks []Mark) error
}

// NewBatchMarkerAWS creates the same marker as NewMarkerAWS, as a BatchMarker.
func NewBatchMarkerAWS(s3Config S3Config, logfn Logf) (BatchMarker, error) {
	marker, err := NewMarkerAWS(s3Config, logfn)
	if err != nil {
		return nil, err
	}
	return marker.(BatchMarker), nil
}

// EndBatch will:
// load all the markers from the file
// ensure the last len(marks) markers are the given marks, in the same order, not ended
// append the end time to all of them
// write the markers back to the file
//
// A batch starts one marker per app while holding the lock, so its markers are the last ones
// of the file. End can't be used one by one: it only ends the very last marker.
func (s *markerAWS) EndBatch(marks []Mark) error {
	s.logfn("[marker] ending %d batch markers", len(marks))
	if len(marks) == 0 {
		return nil
	}
	for _, mark := range marks {
		if mark.Start.IsZero() {
			return ErrNotStartedMark
		}
	}

	markers, err := s.readMarkers()
	if err != nil {
		return err
	}
	if len(markers) < len(marks) {
		return ErrNoStartedMarkersFound
	}

	first := len(markers) - len(marks)
	for i, mark := range marks {
		marker := markers[first+i]
		if !marker.End.IsZero() {
			return fmt.Errorf("%w appName:%s", ErrLastMarkerEnded, marker.AppName)
		}
		if marker.AppName != mark.AppName || !marker.Start.Equals(mark.Start) {
			return fmt.Errorf("%w started:%s appName:%s", ErrNoStartedMarkerFoundForApp, mark.Start, mark.AppName)
		}
	}

	end := CustomTime{s.now()}
	for i := range marks {
		markers[first+i].End = end
	}

	err = s.writeMarkers(markers)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCannotWriteMarkerFile, err)
	}

	return nil
}
