import React from "react";

import ReactTooltip from "react-tooltip";
import TooltipWrapper from "components/TooltipWrapper";

import Button from "components/buttons/Button";
import DiskSpaceGraph from "components/DiskSpaceGraph";
import HumanTimeDiffWithDateTip from "components/HumanTimeDiffWithDateTip";
import { humanHostMemory, wrapFleetHelper } from "utilities/helpers";
import { DEFAULT_EMPTY_CELL_VALUE } from "utilities/constants";
import StatusIndicator from "components/StatusIndicator";
import { IHostMacMdmProfile, BootstrapPackageStatus } from "interfaces/mdm";
import getHostStatusTooltipText from "pages/hosts/helpers";
import PremiumFeatureIconWithTooltip from "components/PremiumFeatureIconWithTooltip";
import IssueIcon from "../../../../../../assets/images/icon-issue-fleet-black-50-16x16@2x.png";
import MacSettingsIndicator from "./MacSettingsIndicator";
import HostSummaryIndicator from "./HostSummaryIndicator";
import BootstrapPackageIndicator from "./BootstrapPackageIndicator/BootstrapPackageIndicator";

const baseClass = "host-summary";

interface IHostDiskEncryptionProps {
  enabled?: boolean;
  tooltip?: string;
}

interface IBootstrapPackageData {
  status?: BootstrapPackageStatus | "";
  details?: string;
}

interface IHostSummaryProps {
  titleData: any; // TODO: create interfaces for this and use consistently across host pages and related helpers
  bootstrapPackageData?: IBootstrapPackageData;
  diskEncryption?: IHostDiskEncryptionProps;
  isPremiumTier?: boolean;
  isSandboxMode?: boolean;
  isOnlyObserver?: boolean;
  toggleOSPolicyModal?: () => void;
  toggleMacSettingsModal?: () => void;
  toggleBootstrapPackageModal?: () => void;
  hostMacSettings?: IHostMacMdmProfile[];
  mdmName?: string;
  showRefetchSpinner: boolean;
  onRefetchHost: (
    evt: React.MouseEvent<HTMLButtonElement, React.MouseEvent>
  ) => void;
  renderActionButtons: () => JSX.Element | null;
  deviceUser?: boolean;
}

const HostSummary = ({
  titleData,
  bootstrapPackageData,
  diskEncryption,
  isPremiumTier,
  isSandboxMode = false,
  isOnlyObserver,
  toggleOSPolicyModal,
  toggleMacSettingsModal,
  toggleBootstrapPackageModal,
  hostMacSettings,
  mdmName,
  showRefetchSpinner,
  onRefetchHost,
  renderActionButtons,
  deviceUser,
}: IHostSummaryProps): JSX.Element => {
  const renderRefetch = () => {
    const isOnline = titleData.status === "online";

    return (
      <>
        <div
          className="refetch"
          data-tip
          data-for="refetch-tooltip"
          data-tip-disable={isOnline || showRefetchSpinner}
        >
          <Button
            className={`
              button
              ${!isOnline ? "refetch-offline tooltip" : ""}
              ${showRefetchSpinner ? "refetch-spinner" : "refetch-btn"}
            `}
            disabled={!isOnline}
            onClick={onRefetchHost}
            variant="text-icon"
          >
            {showRefetchSpinner
              ? "Fetching fresh vitals...this may take a moment"
              : "Refetch"}
          </Button>
        </div>
        <ReactTooltip
          place="top"
          effect="solid"
          id="refetch-tooltip"
          backgroundColor="#3e4771"
        >
          <span className={`${baseClass}__tooltip-text`}>
            You can’t fetch data from <br /> an offline host.
          </span>
        </ReactTooltip>
      </>
    );
  };

  const renderIssues = () => (
    <div className="info-flex__item info-flex__item--title">
      <span className="info-flex__header">
        Issues{isSandboxMode && <PremiumFeatureIconWithTooltip />}
      </span>
      <span className="info-flex__data">
        <span
          className="host-issue tooltip tooltip__tooltip-icon"
          data-tip
          data-for="host-issue-count"
          data-tip-disable={false}
        >
          <img alt="host issue" src={IssueIcon} />
        </span>
        <ReactTooltip
          place="bottom"
          effect="solid"
          backgroundColor="#3e4771"
          id="host-issue-count"
          data-html
        >
          <span className={`tooltip__tooltip-text`}>
            Failing policies ({titleData.issues.failing_policies_count})
          </span>
        </ReactTooltip>
        <span className={"info-flex__data__text"}>
          {titleData.issues.total_issues_count}
        </span>
      </span>
    </div>
  );

  const renderHostTeam = () => (
    <div className="info-flex__item info-flex__item--title">
      <span className="info-flex__header">Team</span>
      <span className={`info-flex__data`}>
        {titleData.team_name ? (
          `${titleData.team_name}`
        ) : (
          <span className="info-flex__no-team">No team</span>
        )}
      </span>
    </div>
  );

  const renderSummary = () => {
    const { status, id } = titleData;
    return (
      <div className="info-flex">
        <div className="info-flex__item info-flex__item--title">
          <span className="info-flex__header">Status</span>
          <StatusIndicator
            value={status || ""} // temporary work around of integration test bug
            tooltip={{
              id,
              tooltipText: getHostStatusTooltipText(status),
              position: "bottom",
            }}
          />
        </div>

        {(titleData.issues?.total_issues_count > 0 || isSandboxMode) &&
          isPremiumTier &&
          renderIssues()}

        {isPremiumTier && renderHostTeam()}

        {titleData.platform === "darwin" &&
          isPremiumTier &&
          mdmName === "Fleet" && // show if 1 - host is enrolled in Fleet MDM, and
          hostMacSettings &&
          hostMacSettings.length > 0 && ( //  2 - host has at least one setting (profile) enforced
            <HostSummaryIndicator title="macOS settings">
              <MacSettingsIndicator
                profiles={hostMacSettings}
                onClick={toggleMacSettingsModal}
              />
            </HostSummaryIndicator>
          )}

        {bootstrapPackageData?.status && (
          <HostSummaryIndicator title="Bootstrap package">
            <BootstrapPackageIndicator
              status={bootstrapPackageData.status}
              onClick={toggleBootstrapPackageModal}
            />
          </HostSummaryIndicator>
        )}

        <div className="info-flex__item info-flex__item--title">
          <span className="info-flex__header">Disk space</span>
          <DiskSpaceGraph
            baseClass="info-flex"
            gigsDiskSpaceAvailable={titleData.gigs_disk_space_available}
            percentDiskSpaceAvailable={titleData.percent_disk_space_available}
            id={`disk-space-tooltip-${titleData.id}`}
            platform={titleData.platform}
            tooltipPosition="bottom"
          />
        </div>

        {typeof diskEncryption?.enabled === "boolean" &&
        diskEncryption?.tooltip ? (
          <div className="info-flex__item info-flex__item--title">
            <span className="info-flex__header">Disk encryption</span>
            <TooltipWrapper
              tipContent={diskEncryption.tooltip}
              position="bottom"
            >
              {diskEncryption.enabled ? "On" : "Off"}
            </TooltipWrapper>
          </div>
        ) : (
          <></>
        )}
        <div className="info-flex__item info-flex__item--title">
          <span className="info-flex__header">Memory</span>
          <span className="info-flex__data">
            {wrapFleetHelper(humanHostMemory, titleData.memory)}
          </span>
        </div>
        <div className="info-flex__item info-flex__item--title">
          <span className="info-flex__header">Processor type</span>
          <span className="info-flex__data">{titleData.cpu_type}</span>
        </div>
        <div className="info-flex__item info-flex__item--title">
          <span className="info-flex__header">Operating system</span>
          <span className="info-flex__data">
            {isOnlyObserver || deviceUser ? (
              `${titleData.os_version}`
            ) : (
              <Button
                onClick={() => toggleOSPolicyModal?.()}
                variant="text-link"
                className={`${baseClass}__os-policy-button`}
              >
                {titleData.os_version}
              </Button>
            )}
          </span>
        </div>
        <div className="info-flex__item info-flex__item--title">
          <span className="info-flex__header">Osquery</span>
          <span className="info-flex__data">{titleData.osquery_version}</span>
        </div>
      </div>
    );
  };

  const lastFetched = titleData.detail_updated_at ? (
    <HumanTimeDiffWithDateTip timeString={titleData.detail_updated_at} />
  ) : (
    ": unavailable"
  );

  return (
    <div className={baseClass}>
      <div className="header title">
        <div className="title__inner">
          <div className="display-name-container">
            <h1 className="display-name">
              {deviceUser
                ? "My device"
                : titleData.display_name || DEFAULT_EMPTY_CELL_VALUE}
            </h1>

            <p className="last-fetched">
              {"Last fetched"} {lastFetched}
              &nbsp;
            </p>
            {renderRefetch()}
          </div>
        </div>
        {renderActionButtons()}
      </div>
      <div className="section title">
        <div className="title__inner">{renderSummary()}</div>
      </div>
    </div>
  );
};

export default HostSummary;
