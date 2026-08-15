# Copyright © 2026 Mindclade, LLC. All Rights Reserved.
# Mindclade Proprietary and Confidential.
# SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
#

from preprocessing.contracts import PipelinePlan


def validate_plan(plan: PipelinePlan) -> PipelinePlan:
    plan.validate()
    return plan
