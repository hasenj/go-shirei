package main

import (
	"fmt"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func deletionAccent() Vec4 {
	if CurrentColorScheme.Surfaces.Panel.Background[2] < 50 {
		return Vec4{28, 85, 72, 1}
	}
	return Vec4{24, 90, 34, 1}
}

func deletionSize(s *Scanner) int {
	total := 0
	for _, e := range s.deleteQueue {
		total += e.Size
	}
	return total
}

func deletionProgress(s *Scanner) string {
	return fmt.Sprintf("Deleting… %d removed · %.1f s", s.deleteRemoved.Load(), time.Since(s.deleteStarted).Seconds())
}

func DeletionQueueStrip(s *Scanner) {
	if len(s.deleteQueue) == 0 {
		return
	}
	accent := deletionAccent()
	bg := accent
	bg[3] = .08
	Container(Attrs(Row, CrossMid, Expand, Pad2(10, 16), Gap(10), BackgroundVec(bg), BorderWidth(1), BorderColorVec(accent)), func() {
		NextAccessName("delete_queue")
		NextAccessValue(fmt.Sprint(len(s.deleteQueue)))
		AssignAccess()
		Icon(TypTrash, FontSize(18), TextColorVec(accent))
		Container(Attrs(Grow(1), Extrinsic, FixHeight(36), Gap(3), Clip), func() {
			Label(fmt.Sprintf("%d queued · %s", len(s.deleteQueue), FmtBytes(deletionSize(s), deletionSize(s))), FontSize(12), FontWeight(WeightSemibold), TextColorVec(accent))
			label := "Nothing has been deleted yet."
			if s.deleteBusy {
				label = deletionProgress(s)
			} else if s.deleteNotice != "" {
				label = s.deleteNotice
			}
			Label(label, FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		})
		NextAccessName("restore_all")
		if ButtonExt("Restore all", ButtonAttrs{Disabled: s.deleteBusy}, ButtonLook{TextSize: 11, PadScale: 1}) {
			restoreDeletion(s, nil)
		}
		NextAccessName("review_deletions")
		label := "Review deletions…"
		if s.deleteBusy {
			label = "View progress…"
		}
		if ButtonExt(label, ButtonAttrs{Disabled: s.state != Done}, ButtonLook{TextSize: 12, PadScale: 1}) {
			s.deleteReview = true
			s.deleteFocus = true
		}
	})
}

func DeleteReviewModal(s *Scanner) {
	if !s.deleteReview || len(s.deleteQueue) == 0 {
		return
	}
	width := min(f32(700), GetHost().WindowSize[0]-32)
	dismiss := func() { s.deleteReview = false }
	Modal(width, dismiss, func() {
		NextAccessName("delete_review")
		NextAccessValue(fmt.Sprint(len(s.deleteQueue)))
		AssignAccess()
		Container(Attrs(Expand, Focusable), func() {
			if s.deleteFocus {
				Focus()
				s.deleteFocus = false
			}
			heading := "Review deletions"
			if s.deleteBusy {
				heading = "Deleting queued items"
			}
			Label(heading, FontSize(18), FontWeight(WeightBold))
		})
		Label(fmt.Sprintf("%d items · %s selected", len(s.deleteQueue), FmtBytes(deletionSize(s), deletionSize(s))), FontSize(13))
		label := "Nothing has been deleted yet."
		if s.deleteBusy {
			label = deletionProgress(s) + ". You can keep browsing while this runs."
		} else if s.deleteNotice != "" {
			label = "Some items remain. Successful deletions are already reflected in the sizes."
		}
		Container(Attrs(MaxWidth(width-40)), func() {
			Label(label, FontSize(12), TextColorVec(CurrentColorScheme.List.Muted))
		})
		Separator()
		items := s.deleteQueue
		Container(Attrs(Expand, MaxHeight(max(100, GetHost().WindowSize[1]-300)), Clip, Gap(8)), func() {
			ScrollOnInput()
			ScrollBars()
			for _, e := range items {
				ContainerWithKey(e, Attrs(Expand, Pad(10), Gap(5), Corners(4), BorderWidth(1), BorderColorVec(CurrentColorScheme.Surfaces.Panel.Border), UseSurface(SurfaceCanvas)), func() {
					NextAccessName("delete_item")
					NextAccessValue(e.Path)
					AssignAccess()
					Container(Attrs(Row, CrossMid, Expand, Gap(8)), func() {
						icon := TypDocument
						if e.IsDir {
							icon = TypFolder
						}
						Icon(icon, FontSize(18))
						Container(Attrs(Grow(1), Extrinsic, Expand, Clip), func() { Label(e.Name, FontSize(13), FontWeight(WeightSemibold)) })
						Label(FmtBytes(e.Size, e.Size), FontSize(12))
						NextAccessName("restore_delete")
						NextAccessValue(e.Path)
						if ButtonExt("Restore", ButtonAttrs{Disabled: s.deleteBusy}, ButtonLook{TextSize: 11, PadScale: 1}) {
							restoreDeletion(s, e)
						}
					})
					Container(Attrs(MaxWidth(width-70)), func() {
						Label(e.Path, FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
						if e.IsDir {
							Label("Folder · includes scanned contents", FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
						}
						if e.deleteErr != nil {
							NextAccessName("delete_error")
							NextAccessValue(e.Path)
							Container(Attrs(MaxWidth(width-70)), func() {
								AssignAccess()
								Label(e.deleteErr.Error(), FontSize(11), TextColorVec(CurrentColorScheme.List.Error))
							})
						}
					})
				})
			}
		})
		Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
			NextAccessName("review_restore_all")
			if ButtonExt("Restore all", ButtonAttrs{Disabled: s.deleteBusy}, ButtonLook{TextSize: 11, PadScale: 1}) {
				restoreDeletion(s, nil)
			}
			Label("Sizes from the measured tree", FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		})
		Separator()
		Label("Permanent deletion cannot be undone.", FontSize(12), TextColorVec(deletionAccent()))
		Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
			Filler(1)
			NextAccessName("delete_back")
			if ButtonExt("Back to scan", ButtonAttrs{}, ButtonLook{TextSize: 12, PadScale: 1}) {
				dismiss()
			}
			label := fmt.Sprintf("Delete %d permanently", len(s.deleteQueue))
			if s.deleteBusy {
				label = "Deleting…"
			}
			NextAccessName("delete_permanently")
			if ButtonExt(label, ButtonAttrs{Type: ButtonDestructive, Disabled: s.deleteBusy || s.state != Done || len(s.deleteQueue) == 0}, ButtonLook{TextSize: 12, PadScale: 1}) {
				commitDeletion(s)
			}
		})
	})
}

func DeleteCloseModal() {
	s := appData.closeQueued
	if s == nil {
		return
	}
	Modal(min(520, GetHost().WindowSize[0]-32), func() { appData.closeQueued = nil }, func() {
		NextAccessName("close_queued")
		AssignAccess()
		Label("Discard this deletion queue?", FontSize(16), FontWeight(WeightSemibold))
		Container(Attrs(MaxWidth(min(480, GetHost().WindowSize[0]-72))), func() {
			Label(fmt.Sprintf("The %d queued items remain on disk. Closing this tab only discards the queue.", len(s.deleteQueue)), FontSize(12))
		})
		Container(Attrs(Row, Expand, Gap(10)), func() {
			Filler(1)
			NextAccessName("keep_queue")
			if Button(NoIcon, "Keep tab open") {
				appData.closeQueued = nil
			}
			NextAccessName("discard_queue_close")
			if Button(NoIcon, "Discard queue and close") {
				appData.closeQueued = nil
				restoreDeletion(s, nil)
				closeScanner(s)
			}
		})
	})
}
