package ant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCover(t *testing.T) {
	s := newSpace()

	s.cover(dataRange{50, 70})
	require.EqualValues(t,
		[]dataRange{
			{50, 70},
		},
		s.covered)

	s.cover(dataRange{20, 25})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{50, 70},
		},
		s.covered)

	s.cover(dataRange{45, 50})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{45, 70},
		},
		s.covered)

	s.cover(dataRange{40, 50})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{40, 70},
		},
		s.covered)

	// already fully covered
	s.cover(dataRange{40, 50})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{40, 70},
		},
		s.covered)

	s.cover(dataRange{70, 75})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{40, 75},
		},
		s.covered)

	s.cover(dataRange{70, 80})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{40, 80},
		},
		s.covered)

	s.cover(dataRange{30, 90})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{30, 90},
		},
		s.covered)

	s.cover(dataRange{100, 110})
	require.EqualValues(t,
		[]dataRange{
			{20, 25},
			{30, 90},
			{100, 110},
		},
		s.covered)

	s.cover(dataRange{22, 90})
	require.EqualValues(t,
		[]dataRange{
			{20, 90},
			{30, 90},
			{100, 110},
		},
		s.covered)
}

func TestCoveredRange(t *testing.T) {
	s := newSpace()
	s.cover(dataRange{20, 40})
	s.cover(dataRange{60, 80})
	s.cover(dataRange{30, 90})
	s.cover(dataRange{100, 120})
	require.EqualValues(t,
		[]dataRange{
			{20, 90},
			{60, 80},
			{100, 120},
		},
		s.covered)

	r := s.coveredRange(10)
	require.Equal(t, zeroRange, r)

	r = s.coveredRange(20)
	require.Equal(t, dataRange{20, 90}, r)

	r = s.coveredRange(30)
	require.Equal(t, dataRange{30, 90}, r)

	r = s.coveredRange(60)
	require.Equal(t, dataRange{60, 90}, r)

	r = s.coveredRange(95)
	require.Equal(t, zeroRange, r)

	r = s.coveredRange(101)
	require.Equal(t, dataRange{101, 120}, r)
}

func TestIsConvered(t *testing.T) {
	s := newSpace()

	s.cover(dataRange{20, 40})
	s.cover(dataRange{60, 80})

	{
		require.False(t, s.isCovered(10))
		require.True(t, s.isCovered(20))
		require.True(t, s.isCovered(21))
		require.True(t, s.isCovered(30))
		require.False(t, s.isCovered(40))
		require.False(t, s.isCovered(41))
		require.False(t, s.isCovered(50))
		require.False(t, s.isCovered(59))
		require.True(t, s.isCovered(60))
		require.True(t, s.isCovered(65))
		require.False(t, s.isCovered(80))
		require.False(t, s.isCovered(100))
	}

	s.cover(dataRange{70, 81})

	{
		require.False(t, s.isCovered(59))
		require.True(t, s.isCovered(60))
		require.True(t, s.isCovered(65))
		require.True(t, s.isCovered(80))
		require.False(t, s.isCovered(81))
		require.False(t, s.isCovered(100))
	}
}
