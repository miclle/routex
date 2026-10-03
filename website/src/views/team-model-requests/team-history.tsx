import { useState } from 'react'
import TeamRequestPanel from './requests'
import { TeamModelReviewPanel } from './index'

export default function TeamModelHistory({
  team,
  reviewer,
  visible,
}: {
  team: string
  reviewer: boolean
  visible: boolean
}) {
  const [reviewMounted, setReviewMounted] = useState(reviewer)
  if (reviewer && !reviewMounted) setReviewMounted(true)
  return (
    <div className="mt-6 space-y-6">
      {reviewMounted && <TeamModelReviewPanel team={team} visible={visible && reviewer} />}
      <TeamRequestPanel ownTeam={team} visible={visible && !reviewer} />
    </div>
  )
}
