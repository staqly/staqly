import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { GalleryView } from "@/components/collections/item-views";
import { ItemPreview } from "@/components/items/item-preview";
import { item } from "./fixtures";
import { mockApi, renderWithProviders } from "./utils";

describe("item rendering by type", () => {
  it("renders uploaded images through the media proxy", () => {
    renderWithProviders(<ItemPreview item={item()} />);
    const img = within(screen.getByTestId("preview-image")).getByRole("img");
    expect(img).toHaveAttribute("src", "/api/v1/media/med_1");
    expect(img).toHaveAttribute("alt", "Avatar v1");
  });

  it("renders external image URLs directly", () => {
    renderWithProviders(<ItemPreview item={item({ media: null, url: "https://cdn.example.com/a.png" })} />);
    expect(within(screen.getByTestId("preview-image")).getByRole("img")).toHaveAttribute(
      "src",
      "https://cdn.example.com/a.png",
    );
  });

  it("renders videos with controls", () => {
    renderWithProviders(
      <ItemPreview
        item={item({
          type: "video",
          media: { id: "med_v", mime_type: "video/mp4", size_bytes: 1, original_name: "clip.mp4", url: "" },
        })}
      />,
    );
    const video = screen.getByTestId("preview-video").querySelector("video")!;
    expect(video).toHaveAttribute("src", "/api/v1/media/med_v");
    expect(video).toHaveAttribute("controls");
  });

  it("renders text preserving whitespace", () => {
    renderWithProviders(<ItemPreview item={item({ type: "text", media: null, content: "line one\n\n  line two" })} />);
    const el = screen.getByTestId("preview-text");
    expect(el.textContent).toBe("line one\n\n  line two");
    expect(el.className).toContain("whitespace-pre-wrap");
  });

  it("pretty-prints JSON", () => {
    renderWithProviders(<ItemPreview item={item({ type: "json", media: null, content: '{"a":1,"b":[true]}' })} />);
    expect(screen.getByTestId("preview-json").textContent).toBe('{\n  "a": 1,\n  "b": [\n    true\n  ]\n}');
  });

  it("renders links as a card with an external link", () => {
    renderWithProviders(
      <ItemPreview
        item={item({
          type: "link",
          media: null,
          title: "Great read",
          url: "https://example.com/post",
          content: "Summary",
        })}
      />,
    );
    const card = screen.getByTestId("preview-link");
    expect(within(card).getByText("example.com")).toBeInTheDocument();
    expect(within(card).getByText("Great read")).toBeInTheDocument();
    expect(within(card).getByRole("link", { name: /open/i })).toHaveAttribute("href", "https://example.com/post");
    expect(within(card).getByText("Summary")).toBeInTheDocument();
  });

  it("offers a download for other files", () => {
    renderWithProviders(
      <ItemPreview
        item={item({
          type: "file",
          media: { id: "med_f", mime_type: "application/pdf", size_bytes: 4096, original_name: "brief.pdf", url: "" },
        })}
      />,
    );
    expect(screen.getByText("brief.pdf")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /download/i })).toHaveAttribute("href", "/api/v1/media/med_f?download=1");
  });
});

describe("gallery view", () => {
  const items = [
    item(),
    item({ id: "itm_2", title: "Caption", type: "text", media: null, content: "Hello there", status: "approved" }),
  ];

  it("shows thumbnails, titles, status and metadata", () => {
    renderWithProviders(<GalleryView items={items} onOpen={() => {}} />);
    const cards = screen.getAllByTestId("gallery-card");
    expect(cards).toHaveLength(2);
    expect(within(cards[0]).getByText("Avatar v1")).toBeInTheDocument();
    expect(within(cards[0]).getByText("Pending")).toBeInTheDocument();
    expect(within(cards[0]).getByText("studio portrait")).toBeInTheDocument();
    expect(cards[0].querySelector("img")).toHaveAttribute("src", "/api/v1/media/med_1");
    expect(within(cards[1]).getByText("Approved")).toBeInTheDocument();
    expect(within(cards[1]).getByText("Hello there")).toBeInTheDocument();
    // Quick review only for pending items
    expect(within(cards[0]).getByRole("button", { name: "Approve Avatar v1" })).toBeInTheDocument();
    expect(within(cards[1]).queryByRole("button", { name: /approve/i })).not.toBeInTheDocument();
  });

  it("opens an item on click and approves from the card", async () => {
    const api = mockApi({
      "POST /v1/items/itm_1/actions/approve": item({ status: "approved", version: 2 }),
      "GET /v1/collections": { data: [] },
      "GET /v1/events": { data: [] },
    });
    const opened: string[] = [];
    const user = userEvent.setup();
    renderWithProviders(<GalleryView items={items} onOpen={(i) => opened.push(i.id)} />);
    await user.click(screen.getByText("Caption"));
    expect(opened).toEqual(["itm_2"]);
    await user.click(screen.getByRole("button", { name: "Approve Avatar v1" }));
    await waitFor(() => expect(api.find("POST", "/v1/items/itm_1/actions/approve")).toHaveLength(1));
    expect(api.find("POST", "/v1/items/itm_1/actions/approve")[0].body).toEqual({ expected_version: 1 });
    expect(opened).toEqual(["itm_2"]); // quick action does not open the item
  });
});
